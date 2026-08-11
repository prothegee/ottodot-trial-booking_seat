package roster

import "context"

// Reader is the only way this package reaches storage.
//
// It exists as an interface so the fast test tiers can prove the shape of a
// roster, and the ordering of its seats, without replication running to answer a
// question that has nothing to do with replication.
type Reader interface {
    // For reads everyone who owns a seat in one class.
    //
    // It reports ErrClassNotFound when there is no such class, which is
    // different from a class nobody has booked: that one answers with an empty
    // roster, because an empty class is a real answer a teacher needs.
    For(ctx context.Context, classID string) (Roster, error)
}
