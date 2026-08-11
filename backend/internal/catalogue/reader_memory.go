package catalogue

import (
    "context"
    "errors"
    "sort"
    "sync"
)

// errStorageUnusable is what a reader told to fail reports.
//
// It is deliberately not one of this package's sentinels. The http layer maps
// every sentinel it knows to a code a parent can act on, and this one has to
// fall through to internal_error, which is exactly the path the case that uses
// it is about.
var errStorageUnusable = errors.New("catalogue: the reader was told to fail")

// MemoryReader is the fake every fast test runs against.
//
// It counts its own reads. That count is the whole reason this type has a method
// the interface does not: a conditional request that skips the database can only
// be proven by something that would have noticed being asked.
type MemoryReader struct {
    mutex   sync.Mutex
    classes map[string]Class
    reads   int
    failing bool
}

// NewMemoryReader builds an empty catalogue. Classes are put in with AddClass,
// because a fake with rows already in it hides what a test depends on.
func NewMemoryReader() *MemoryReader {
    return &MemoryReader{classes: make(map[string]Class)}
}

// AddClass puts a class in front of the reader, or replaces one already there.
// Replacing is how a test moves a seat count without a booking transaction.
func (reader *MemoryReader) AddClass(class Class) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    reader.classes[class.ID] = class
}

// Reads is how many times storage was actually asked. It is the fake's own
// record, and it is what a conditional request test asserts against.
func (reader *MemoryReader) Reads() int {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    return reader.reads
}

// FailNext makes the next read report the storage as unusable.
//
// It exists for one case: the internal_error path. That is the only refusal in
// the api that carries a request id and no detail at all, and it cannot be
// reached at all without something breaking underneath. The failure is spent
// after one read, so a case cannot accidentally leave a reader broken for the
// assertions that follow.
func (reader *MemoryReader) FailNext() {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    reader.failing = true
}

// spendFailure reports whether this read is the one that was told to break.
func (reader *MemoryReader) spendFailure() bool {
    if !reader.failing {
        return false
    }

    reader.failing = false

    return true
}

// Classes lists every class, soonest first.
func (reader *MemoryReader) Classes(_ context.Context) ([]Class, error) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    if reader.spendFailure() {
        return nil, errStorageUnusable
    }

    reader.reads++

    listed := make([]Class, 0, len(reader.classes))

    for _, class := range reader.classes {
        listed = append(listed, class)
    }

    // Soonest first, with the identifier breaking a tie. Two classes starting at
    // the same instant would otherwise come back in map order, which changes
    // between runs and would change the etag of an unchanged catalogue.
    sort.Slice(listed, func(first int, second int) bool {
        if !listed[first].StartsAt.Equal(listed[second].StartsAt) {
            return listed[first].StartsAt.Before(listed[second].StartsAt)
        }

        return listed[first].ID < listed[second].ID
    })

    return listed, nil
}

// Class reads one class.
func (reader *MemoryReader) Class(_ context.Context, classID string) (Class, error) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    if reader.spendFailure() {
        return Class{}, errStorageUnusable
    }

    reader.reads++

    class, found := reader.classes[classID]
    if !found {
        return Class{}, ErrClassNotFound
    }

    return class, nil
}
