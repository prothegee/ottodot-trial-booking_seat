package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDirectory is the real identity read.
type PostgresDirectory struct {
	pool *pgxpool.Pool
}

// NewPostgresDirectory wraps a pool.
//
// Note:
//   - the pool must be the primary. Both reads decide whether a request may
//     proceed, and the replica is asynchronous, so an account created a moment
//     ago could sign in against one connection and not the other.
func NewPostgresDirectory(pool *pgxpool.Pool) *PostgresDirectory {
	return &PostgresDirectory{pool: pool}
}

// ParentByEmail is the sign in lookup.
//
// The comparison is on lower(email) rather than on email, so an address typed
// with a capital letter reaches the same account. The fake normalises the same
// way, which is what keeps the two from disagreeing about who exists.
func (directory *PostgresDirectory) ParentByEmail(ctx context.Context, email string) (Parent, error) {
	normalised := normaliseEmail(email)
	if normalised == "" {
		return Parent{}, ErrInvalidRequest
	}

	var found Parent

	err := directory.pool.QueryRow(ctx,
		`select id, full_name, role from parents where lower(email) = $1`,
		normalised).Scan(&found.ID, &found.DisplayName, &found.Role)
	if err != nil {
		return Parent{}, translate(err, ErrNoSuchParent)
	}

	return found, nil
}

// Account is the session read: the parent, then the children on that account.
//
// Two queries rather than one join. A join would repeat the parent row once per
// child and leave this code unpicking it, and the two reads are per request on
// a route that is called on a page load rather than on every call.
func (directory *PostgresDirectory) Account(ctx context.Context, parentID string) (Account, error) {
	if parentID == "" {
		return Account{}, ErrInvalidRequest
	}

	var found Parent

	err := directory.pool.QueryRow(ctx,
		`select id, full_name, role from parents where id = $1`,
		parentID).Scan(&found.ID, &found.DisplayName, &found.Role)
	if err != nil {
		return Account{}, translate(err, ErrNoSuchParent)
	}

	rows, err := directory.pool.Query(ctx,
		`select id, full_name, grade_level from students where parent_id = $1 order by full_name, id`,
		parentID)
	if err != nil {
		return Account{}, fmt.Errorf("the children on the account could not be read: %w", translate(err, nil))
	}

	defer rows.Close()

	children := make([]Child, 0)

	for rows.Next() {
		var child Child

		if err := rows.Scan(&child.ID, &child.FullName, &child.GradeLevel); err != nil {
			return Account{}, err
		}

		children = append(children, child)
	}

	if err := rows.Err(); err != nil {
		return Account{}, translate(err, nil)
	}

	return Account{Parent: found, Children: children}, nil
}
