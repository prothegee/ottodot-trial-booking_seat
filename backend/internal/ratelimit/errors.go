package ratelimit

import "errors"

// The failures this package can report.
//
// Being over the limit is not among them. That is an ordinary answer carried in
// a Decision, not a failure, because the caller has to tell "you asked too
// often" apart from "the limiter is broken" and answer the two differently.
var (
    // ErrInvalidRequest means the call was refused before any state was read:
    // an empty key, a burst below one, or an interval of zero.
    ErrInvalidRequest = errors.New("ratelimit: the request is missing something it needs")

    // ErrUnavailable means the shared state could not be reached.
    //
    // It exists because this is the one place in the service where failing open
    // and failing closed are both defensible, and the choice has to be made per
    // route rather than buried here. See the middleware for which one each
    // route takes and why.
    ErrUnavailable = errors.New("ratelimit: the limiter could not be reached")
)
