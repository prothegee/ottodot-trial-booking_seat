package observability

import "context"

// failureDetailKey is the context key the detail is carried under.
//
// It is an unexported type rather than a string, so nothing outside this package
// can put a value there. A detail a caller could plant is a detail that cannot be
// trusted to describe the failure it is logged next to.
type failureDetailKey struct{}

// FailureDetail is the error behind one refusal, on its way to the log.
//
// The client is told a code and nothing else, which is the right answer for the
// client and a dead end for whoever has to fix it. This is how the part that is
// not sent gets from the handler that raised it to the middleware that writes it
// down.
//
// Note:
//   - it holds the first error recorded and ignores later ones. One request is
//     one answer, and the first refusal is the one that caused it.
//   - it is written from the goroutine serving the request and read after that
//     handler has returned, so it needs no lock of its own.
type FailureDetail struct {
    err error
}

// WithFailureDetail puts an empty detail on a context and hands back both.
//
// Param:
// parent - context.Context (the context to derive from)
//
// Return:
//   - the derived context, which every handler below it shares
//   - the detail, to be read once the handler has returned
func WithFailureDetail(parent context.Context) (context.Context, *FailureDetail) {
    detail := &FailureDetail{}

    return context.WithValue(parent, failureDetailKey{}, detail), detail
}

// RecordFailureDetail writes the error behind a refusal where the middleware can
// find it.
//
// A context carrying nothing is not a failure. It is what a test that does not
// care about logging gets, and it must never be the reason a request is answered
// differently.
//
// Param:
// ctx - context.Context (the request's context)
// err - error (what actually failed)
func RecordFailureDetail(ctx context.Context, err error) {
    if err == nil {
        return
    }

    detail, carried := ctx.Value(failureDetailKey{}).(*FailureDetail)
    if !carried || detail.err != nil {
        return
    }

    detail.err = err
}

// Err is what was recorded.
//
// Return:
//   - the error behind the refusal
//   - nil when the request was answered without one
func (detail *FailureDetail) Err() error {
    return detail.err
}
