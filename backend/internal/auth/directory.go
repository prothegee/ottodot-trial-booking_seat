package auth

import "context"

// Directory answers the two identity questions this package asks.
//
// Sign in asks who owns an email. The session endpoint asks who a parent id is
// and which children are on that account. Nothing else about a parent is read
// here, and nothing here writes.
//
// It is separate from RefreshStore because the two answer different questions
// and change for different reasons: one is the token lifecycle, the other is
// who the account belongs to.
type Directory interface {
    // CredentialByEmail is the sign in lookup. It reports ErrNoSuchParent when
    // the address is not one of the seeded accounts.
    CredentialByEmail(ctx context.Context, email string) (Credential, error)

    // Account is the session read: the parent, and the children on their
    // account. It reports ErrNoSuchParent when the id matches nothing.
    Account(ctx context.Context, parentID string) (Account, error)
}

// Parent is who a token speaks for.
//
// There is no email field, and its absence is the design. Nothing downstream
// needs one: the token carries an id and a role, the session response carries a
// display name, and an email that is never loaded is an email that can never be
// logged, put in a claim, or returned by accident.
type Parent struct {
    ID string

    // DisplayName is what the client greets the parent with. It is the one
    // piece of naming that leaves this service, and it never reaches a token,
    // a metric label, or a log line.
    DisplayName string

    // Role is parent or admin, and it is what the admin routes are gated on.
    Role string
}

// Credential is an account as a sign in attempt needs to see it: who it would
// be, and what the typed password is checked against.
//
// It is a separate type from Parent, and the separation is the design. The hash
// is read on exactly one path and sits on no struct a handler could put into a
// response by reflex.
type Credential struct {
    Parent Parent

    // PasswordHash is the stored argon2id string. An account holding one that
    // is empty or unreadable can never be signed in to, which is the answer a
    // half finished seed should give.
    PasswordHash string
}

// Child is one student on a parent's account.
type Child struct {
    ID         string
    FullName   string
    GradeLevel int16
}

// Account is the whole answer to "who am I signed in as".
type Account struct {
    Parent   Parent
    Children []Child
}
