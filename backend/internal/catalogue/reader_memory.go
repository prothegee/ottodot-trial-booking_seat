package catalogue

import (
    "context"
    "sort"
    "sync"
)

// MemoryReader is the fake every fast test runs against.
//
// It counts its own reads. That count is the whole reason this type has a method
// the interface does not: a conditional request that skips the database can only
// be proven by something that would have noticed being asked.
type MemoryReader struct {
    mutex   sync.Mutex
    classes map[string]Class
    reads   int
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

// Classes lists every class, soonest first.
func (reader *MemoryReader) Classes(_ context.Context) ([]Class, error) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

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

    reader.reads++

    class, found := reader.classes[classID]
    if !found {
        return Class{}, ErrClassNotFound
    }

    return class, nil
}
