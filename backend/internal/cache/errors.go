package cache

import "errors"

// The failures this package can report.
//
// There is no "not found" among them. A miss is not a failure, it is the
// ordinary answer, so every read reports it as a boolean instead. An error here
// always means the store itself could not be used.
var (
    // ErrInvalidRequest means the call was refused before the store was
    // touched: an empty key, a lifetime of zero, or an entry with no tag.
    ErrInvalidRequest = errors.New("cache: the request is missing something it needs")

    // ErrUnavailable means the store could not be reached.
    //
    // It exists so a caller can tell "the cache said no" apart from "the cache
    // said nothing". Every read path in this service treats it as a miss and
    // goes to the database, because caching is an optimisation and never a
    // dependency.
    ErrUnavailable = errors.New("cache: the store could not be reached")
)
