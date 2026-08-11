package roster

import (
    "context"
    "sort"
    "sync"
)

// MemoryReader is the fake every fast test runs against.
type MemoryReader struct {
    mutex      sync.Mutex
    capacities map[string]int16
    entries    map[string][]Entry
}

// NewMemoryReader builds an empty reader. Classes are put in with AddClass,
// because a fake with rows already in it hides what a test depends on.
func NewMemoryReader() *MemoryReader {
    return &MemoryReader{
        capacities: make(map[string]int16),
        entries:    make(map[string][]Entry),
    }
}

// AddClass declares a class exists, which is what makes an empty roster
// different from a class nobody has.
func (reader *MemoryReader) AddClass(classID string, capacity int16) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    reader.capacities[classID] = capacity
}

// AddEntry seats one child. It stands in for a confirmed booking.
func (reader *MemoryReader) AddEntry(classID string, entry Entry) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    reader.entries[classID] = append(reader.entries[classID], entry)
}

// For reads everyone who owns a seat in one class.
func (reader *MemoryReader) For(_ context.Context, classID string) (Roster, error) {
    reader.mutex.Lock()
    defer reader.mutex.Unlock()

    capacity, found := reader.capacities[classID]
    if !found {
        return Roster{}, ErrClassNotFound
    }

    seated := make([]Entry, len(reader.entries[classID]))
    copy(seated, reader.entries[classID])

    sort.Slice(seated, func(first int, second int) bool {
        return seated[first].SeatNo < seated[second].SeatNo
    })

    return Roster{ClassID: classID, Capacity: capacity, Entries: seated}, nil
}
