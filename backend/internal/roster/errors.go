package roster

import "errors"

// The failures this package can report.
//
// No message carries a name or an identifier. That rule is stricter here than
// anywhere else in this service, because this is the one package that holds
// child names, and an error string is the easiest way for one to reach a log.
var (
    // ErrInvalidRequest means the read was refused before anything was looked
    // up.
    ErrInvalidRequest = errors.New("roster: the request is missing something it needs")

    // ErrClassNotFound means no class carries that id.
    ErrClassNotFound = errors.New("roster: no such class")
)
