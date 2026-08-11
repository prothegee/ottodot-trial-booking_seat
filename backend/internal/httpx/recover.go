package httpx

import (
    "fmt"
    "net/http"
)

// ReportFunc is where a recovered panic is written down.
//
// It is a function rather than a logger so this package depends on nothing to
// log, and so a test can prove the report happened without reading a stream.
type ReportFunc func(requestID string, err error)

// Recover turns a panic into the one failure a client can be told about.
//
// A panic in a handler kills the connection and, without this, takes the whole
// process with it. Neither is a good answer for one parent's request. The
// process keeps serving, the parent gets internal_error with a request id, and
// the detail goes where detail belongs.
//
// Note:
//   - nothing about the panic reaches the client. A panic message can carry a
//     row, a query, or a value from somebody's account.
//   - the response is only written when nothing has been written yet. A panic
//     halfway through a body cannot be turned into a clean 500, so the
//     connection is allowed to break rather than have an envelope appended to
//     half a document.
//
// Param:
// report - ReportFunc (where the detail goes, nil for nowhere)
//
// Return:
//   - the middleware
func Recover(report ReportFunc) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
            tracked := &statusWriter{ResponseWriter: response}

            defer func() {
                recovered := recover()
                if recovered == nil {
                    return
                }

                if report != nil {
                    report(RequestIDFrom(request.Context()), fmt.Errorf("a handler panicked: %v", recovered))
                }

                if tracked.written {
                    return
                }

                WriteFailure(tracked, request, Failure{
                    Status:  http.StatusInternalServerError,
                    Code:    CodeInternalError,
                    Message: "something went wrong on our side",
                })
            }()

            next.ServeHTTP(tracked, request)
        })
    }
}

// statusWriter remembers whether anything has gone out yet.
//
// It is the smallest thing that answers the only question the recovery needs:
// can a failure still be written, or is the response already half sent.
type statusWriter struct {
    http.ResponseWriter

    written bool
    status  int
}

// WriteHeader records the status and passes it on.
func (writer *statusWriter) WriteHeader(status int) {
    if writer.written {
        return
    }

    writer.written = true
    writer.status = status

    writer.ResponseWriter.WriteHeader(status)
}

// Write records that a body has started, since a body written without an
// explicit status is a 200 that was never announced.
func (writer *statusWriter) Write(payload []byte) (int, error) {
    if !writer.written {
        writer.written = true
        writer.status = http.StatusOK
    }

    return writer.ResponseWriter.Write(payload)
}

// Status is what went out, or zero when nothing has.
func (writer *statusWriter) Status() int {
    return writer.status
}
