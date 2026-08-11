package catalogue

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording. No message carries an identifier or a
// name, because these strings reach a log and a log gets pasted into a chat
// window.
var (
    // ErrInvalidRequest means the read was refused before anything was looked
    // up.
    ErrInvalidRequest = errors.New("catalogue: the request is missing something it needs")

    // ErrClassNotFound means no class carries that id.
    ErrClassNotFound = errors.New("catalogue: no such class")
)
