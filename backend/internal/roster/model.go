// Package roster answers one question for a teacher: who is actually coming to
// this class, and in which seat.
//
// It is the only read in this service that puts a child's name next to a seat,
// which is why it is a package of its own rather than a method on something
// else. A name is the most sensitive thing here, and keeping the query that
// returns one in a package nothing parent facing imports is what makes "a parent
// can never reach a roster" a property of the code rather than of a route table
// somebody has to remember to check.
//
// It reads the replica. A teacher opens this minutes before a class starts, not
// at the instant of a write, so lag of a second is invisible and the pool the
// confirm transaction needs stays free.
package roster

import "time"

// Entry is one child in one seat.
//
// Only confirmed bookings appear. A hold is not attendance, and a teacher
// printing this list needs the people who own a seat, not the people who were
// looking at a payment screen when the query ran.
type Entry struct {
    SeatNo      int16
    StudentID   string
    StudentName string
    ConfirmedAt time.Time
}

// Roster is one class and everyone who owns a seat in it.
type Roster struct {
    ClassID  string
    Capacity int16

    // Entries are ordered by seat number, ascending. A teacher reads down a
    // list, so the order is part of the answer rather than a detail.
    Entries []Entry
}

// SeatsTaken is how many seats are owned. It is exact rather than advisory,
// because a confirmed booking is a decision that already happened.
func (roster Roster) SeatsTaken() int {
    return len(roster.Entries)
}
