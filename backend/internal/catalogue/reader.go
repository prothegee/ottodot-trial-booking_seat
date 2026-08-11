package catalogue

import "context"

// Reader is the only way this package reaches storage.
//
// It exists as an interface for two reasons. The fast test tiers run against a
// fake, so a conditional request can be proven to skip storage entirely by
// counting calls on it. And the real implementation is bound to the replica,
// which means a test that wanted to use it would need replication running to
// prove something that has nothing to do with replication.
//
// Every method here is a read. There is no write in this package at all, which
// is what makes the replica the right pool for it.
type Reader interface {
    // Classes lists every trial class with the seats it has left, soonest
    // first.
    Classes(ctx context.Context) ([]Class, error)

    // Class reads one. It reports ErrClassNotFound when there is none.
    Class(ctx context.Context, classID string) (Class, error)
}
