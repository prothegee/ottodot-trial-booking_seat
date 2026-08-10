package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres error codes this store turns into its own failures. A driver code
// reaching a caller would make the auth surface depend on the database vendor,
// which is a coupling nobody notices until it is expensive.
const (
	uniqueViolation           = "23505"
	invalidTextRepresentation = "22P02"
)

// refreshColumns is the read shape for one stored token.
const refreshColumns = `id, parent_id, token_hash, family_id, expires_at, revoked_at, created_at`

// PostgresRefreshStore is the real one.
type PostgresRefreshStore struct {
	pool *pgxpool.Pool
}

// NewPostgresRefreshStore wraps a pool.
//
// Note:
//   - the pool must be the primary. Every method here either writes or decides
//     whether a token has already been spent, and the replica is asynchronous,
//     so a rotation judged against it could accept the same token twice.
func NewPostgresRefreshStore(pool *pgxpool.Pool) *PostgresRefreshStore {
	return &PostgresRefreshStore{pool: pool}
}

// Issue writes the first token of a new family.
func (store *PostgresRefreshStore) Issue(ctx context.Context, request IssueRequest) (RefreshRecord, error) {
	if err := request.Validate(); err != nil {
		return RefreshRecord{}, err
	}

	written, err := scanRefresh(store.pool.QueryRow(ctx, `
		insert into refresh_tokens (id, parent_id, token_hash, family_id, expires_at, created_at)
		values ($1, $2, $3, $4, $5, $6)
		returning `+refreshColumns,
		request.TokenID, request.ParentID, request.TokenHash,
		request.FamilyID, request.ExpiresAt, request.Now))
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("the refresh token could not be written: %w", translate(err, nil))
	}

	return written, nil
}

// Rotate spends the presented token and writes its successor.
//
// The whole decision is one transaction, and the row is taken with FOR UPDATE
// before anything is judged. That ordering is the point:
//
//	the select locks the presented row, so a second refresh with the same
//	token waits here rather than reading the same live row
//	the first caller revokes it and inserts the successor
//	the second caller then reads a revoked row and reports reuse
//
// Without the lock both callers read a live row, both rotate, and one stolen
// token quietly becomes two working sessions.
func (store *PostgresRefreshStore) Rotate(ctx context.Context, request RotateRequest) (RefreshRecord, error) {
	if err := request.Validate(); err != nil {
		return RefreshRecord{}, err
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("the rotation could not start: %w", translate(err, nil))
	}

	defer func() {
		// Rollback after a commit is a no-op, so this is the safe shape rather
		// than a branch that has to be kept in step with every return below.
		_ = transaction.Rollback(ctx)
	}()

	presented, err := scanRefresh(transaction.QueryRow(ctx,
		`select `+refreshColumns+` from refresh_tokens where token_hash = $1 for update`,
		request.PresentedHash))
	if err != nil {
		return RefreshRecord{}, translate(err, ErrTokenNotFound)
	}

	if presented.IsRevoked() {
		if _, err := revokeFamilyIn(ctx, transaction, presented.FamilyID, request.Now); err != nil {
			return RefreshRecord{}, err
		}

		// The revocation is committed even though the caller is refused. A
		// rollback here would detect the theft and then undo the response to
		// it.
		if err := transaction.Commit(ctx); err != nil {
			return RefreshRecord{}, fmt.Errorf("the family revocation could not be committed: %w", translate(err, nil))
		}

		return RefreshRecord{}, ErrTokenReused
	}

	if presented.IsExpired(request.Now) {
		return RefreshRecord{}, ErrTokenExpired
	}

	if _, err := transaction.Exec(ctx,
		`update refresh_tokens set revoked_at = $2 where id = $1`,
		presented.ID, request.Now); err != nil {
		return RefreshRecord{}, fmt.Errorf("the spent token could not be revoked: %w", translate(err, nil))
	}

	successor, err := scanRefresh(transaction.QueryRow(ctx, `
		insert into refresh_tokens (id, parent_id, token_hash, family_id, expires_at, created_at)
		values ($1, $2, $3, $4, $5, $6)
		returning `+refreshColumns,
		request.NextTokenID, presented.ParentID, request.NextTokenHash,
		presented.FamilyID, request.NextExpiresAt, request.Now))
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("the successor token could not be written: %w", translate(err, nil))
	}

	if err := transaction.Commit(ctx); err != nil {
		return RefreshRecord{}, fmt.Errorf("the rotation could not be committed: %w", translate(err, nil))
	}

	return successor, nil
}

// RevokeFamily ends every live token in one chain.
func (store *PostgresRefreshStore) RevokeFamily(ctx context.Context, request RevokeFamilyRequest) (int, error) {
	if err := request.Validate(); err != nil {
		return 0, err
	}

	ended, err := revokeFamilyIn(ctx, store.pool, request.FamilyID, request.Now)
	if err != nil {
		return 0, err
	}

	return ended, nil
}

// Record reads one token by its hash.
func (store *PostgresRefreshStore) Record(ctx context.Context, tokenHash []byte) (RefreshRecord, error) {
	if len(tokenHash) == 0 {
		return RefreshRecord{}, ErrInvalidRequest
	}

	stored, err := scanRefresh(store.pool.QueryRow(ctx,
		`select `+refreshColumns+` from refresh_tokens where token_hash = $1`, tokenHash))
	if err != nil {
		return RefreshRecord{}, translate(err, ErrTokenNotFound)
	}

	return stored, nil
}

// executor is whatever can run a statement: the pool, or a transaction inside
// one. It exists so the family revocation is written once and used from both.
type executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// revokeFamilyIn ends every live token in one chain and reports the count.
//
// The `revoked_at is null` condition is what leaves already revoked rows at the
// instant they were revoked, so the audit reads as what happened rather than as
// what the last call did.
func revokeFamilyIn(ctx context.Context, runner executor, familyID string, now time.Time) (int, error) {
	tag, err := runner.Exec(ctx,
		`update refresh_tokens set revoked_at = $2 where family_id = $1 and revoked_at is null`,
		familyID, now)
	if err != nil {
		return 0, fmt.Errorf("the token family could not be revoked: %w", translate(err, nil))
	}

	return int(tag.RowsAffected()), nil
}

// scanRefresh reads one row, turning the nullable revocation into a zero value
// so no caller carries a nil check.
func scanRefresh(row pgx.Row) (RefreshRecord, error) {
	var (
		stored    RefreshRecord
		revokedAt *time.Time
	)

	err := row.Scan(&stored.ID, &stored.ParentID, &stored.TokenHash,
		&stored.FamilyID, &stored.ExpiresAt, &revokedAt, &stored.CreatedAt)
	if err != nil {
		return RefreshRecord{}, err
	}

	if revokedAt != nil {
		stored.RevokedAt = *revokedAt
	}

	return stored, nil
}

// translate turns a driver failure into one of this package's own failures.
//
// Param:
// err - error (whatever the driver reported)
// missing - error (what an empty result means here, nil when it cannot happen)
//
// Return:
//   - the package failure when the cause is one this package names
//   - the original error otherwise, so nothing unexpected is silently reshaped
func translate(err error, missing error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) && missing != nil {
		return missing
	}

	var databaseError *pgconn.PgError

	if !errors.As(err, &databaseError) {
		return err
	}

	switch databaseError.Code {
	case uniqueViolation:
		return ErrDuplicateToken
	case invalidTextRepresentation:
		// A malformed uuid never matches a row, so the honest answer is the
		// same one an absent row gets rather than a driver message about
		// syntax.
		if missing != nil {
			return missing
		}

		return ErrInvalidRequest
	}

	return err
}
