package httpx

import (
    "encoding/json"
    "net/http"
    "strconv"

    "ottodot-trial-booking/backend/internal/observability"
)

// The two cache policies this api uses on a successful read.
//
// Everything else is no-store. A booking, a payment, a roster, and every auth
// route decide something or belong to one parent, and a cached copy of any of
// them served to the next person is the failure that matters here.
const (
    // cacheableReadPolicy goes on the two class reads. The short max-age lets a
    // browser skip a request during one burst of clicking, and
    // stale-while-revalidate lets it paint immediately while it checks.
    cacheableReadPolicy = "private, max-age=5, stale-while-revalidate=30"

    // privateReadPolicy goes on a read that belongs to one parent and is worth
    // holding for a moment. It is private so no shared proxy keeps a copy.
    privateReadPolicy = "private, max-age=5"

    // noStorePolicy goes on everything else.
    noStorePolicy = "no-store"
)

// envelope is the one failure shape the whole api answers with.
type envelope struct {
    Error envelopeBody `json:"error"`
}

// envelopeBody carries the code the client switches on and prose no client
// reads.
//
// Every optional field is omitted when empty rather than sent as null, so the
// ordinary refusal is three lines and a reader can see at a glance that nothing
// else was attached.
type envelopeBody struct {
    Code              string `json:"code"`
    Message           string `json:"message"`
    RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
    RequestID         string `json:"request_id,omitempty"`
    BookingID         string `json:"booking_id,omitempty"`
}

// WriteFailure answers one request with the envelope.
//
// Note:
//   - the request id is attached only to internal_error. It is the one code a
//     client cannot act on, so the id is the whole of what it is told, and
//     putting an id on every refusal would invite a client to log all of them.
//   - a rate limit refusal also carries the standard Retry-After header, so a
//     client library that already understands one does the right thing without
//     reading the body.
//
// Param:
// response - http.ResponseWriter (the response being written)
// request - *http.Request (where the request id is read from)
// failure - Failure (the status, code, and wording)
func WriteFailure(response http.ResponseWriter, request *http.Request, failure Failure) {
    body := envelopeBody{
        Code:              failure.Code,
        Message:           failure.Message,
        RetryAfterSeconds: failure.RetryAfterSeconds,
        BookingID:         failure.BookingID,
    }

    if failure.Code == CodeInternalError {
        body.RequestID = RequestIDFrom(request.Context())
    }

    if failure.RetryAfterSeconds > 0 {
        response.Header().Set("Retry-After", strconv.Itoa(failure.RetryAfterSeconds))
    }

    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", noStorePolicy)
    response.WriteHeader(failure.Status)

    // The body is a handful of short strings this package built, so a failed
    // encode is not a case that can happen. Answering a refusal with a second
    // refusal about the first one helps nobody.
    _ = json.NewEncoder(response).Encode(envelope{Error: body})
}

// Deny answers one request with whatever that failure looks like on the wire.
func Deny(response http.ResponseWriter, request *http.Request, err error) {
    denyWith(response, request, FailureFor(err), err)
}

// denyWith answers a refusal whose shape was already decided, keeping the error
// behind it for the log.
//
// It exists because a caller that adjusts the failure it is about to send still
// holds the only copy of what actually went wrong, and dropping that on the way
// out is how a 500 ends up with nothing but a request id.
//
// Param:
// response - http.ResponseWriter (the response being written)
// request - *http.Request (where the detail is recorded)
// failure - Failure (what the client is told)
// err - error (what actually failed, never sent)
func denyWith(response http.ResponseWriter, request *http.Request, failure Failure, err error) {
    if failure.Code == CodeInternalError {
        observability.RecordFailureDetail(request.Context(), err)
    }

    WriteFailure(response, request, failure)
}

// writeJSON answers a read or a write that succeeded.
//
// Param:
// response - http.ResponseWriter (the response being written)
// status - int (the status to answer with)
// policy - string (the Cache-Control value, one of the three above)
// body - any (what to encode)
func writeJSON(response http.ResponseWriter, status int, policy string, body any) {
    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", policy)
    response.WriteHeader(status)

    _ = json.NewEncoder(response).Encode(body)
}

// writeBytes answers with a body that was already encoded.
//
// It exists for the cache: a stored response is bytes and a tag, and re-encoding
// it would produce a different byte order for the same data and therefore a
// different tag for an unchanged document.
func writeBytes(response http.ResponseWriter, status int, policy string, etag string, payload []byte) {
    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", policy)

    if etag != "" {
        response.Header().Set("ETag", etag)
    }

    response.WriteHeader(status)

    _, _ = response.Write(payload)
}

// noContent answers a write that succeeded and has nothing to say.
func noContent(response http.ResponseWriter) {
    response.Header().Set("Cache-Control", noStorePolicy)
    response.WriteHeader(http.StatusNoContent)
}
