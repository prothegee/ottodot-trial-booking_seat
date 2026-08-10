package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

// The error codes this package answers with.
//
// They are a closed set, taken from the one envelope table the whole api uses,
// so the client switches on a code it already knows rather than on prose. A new
// code here is a change to that contract and to the frontend that reads it.
const (
	CodeInvalidRequest = "invalid_request"
	CodeTokenExpired   = "token_expired"
	CodeTokenInvalid   = "token_invalid"
	CodeTokenReused    = "token_reused"
	CodeForbiddenRole  = "forbidden_role"
	CodeInternalError  = "internal_error"
)

// Failure is one refusal as it appears on the wire.
type Failure struct {
	Status  int
	Code    string
	Message string
}

// FailureFor turns a failure from this package into the answer a client gets.
//
// Note:
//   - ErrNoSuchParent becomes the generic refusal rather than a code of its
//     own. An endpoint that answers differently for a known address is an
//     endpoint that lists who has an account here.
//   - ErrOriginRefused becomes the generic refusal for the same kind of reason:
//     the envelope is a closed set the client already maps, and a cross-origin
//     write is not something a real client ever sends, so it needs no code of
//     its own.
//   - anything this package does not name becomes internal_error, which
//     carries no detail. A driver message reaching a client is how a table name
//     ends up in a screenshot.
//
// Param:
// err - error (whatever failed)
//
// Return:
//   - the status, code, and wording for that failure
func FailureFor(err error) Failure {
	switch {
	case errors.Is(err, ErrTokenExpired):
		return Failure{http.StatusUnauthorized, CodeTokenExpired,
			"your session needs refreshing"}

	case errors.Is(err, ErrTokenReused):
		return Failure{http.StatusUnauthorized, CodeTokenReused,
			"this sign in was ended for safety, please sign in again"}

	case errors.Is(err, ErrTokenInvalid), errors.Is(err, ErrTokenNotFound), errors.Is(err, ErrNotAuthenticated):
		return Failure{http.StatusUnauthorized, CodeTokenInvalid,
			"please sign in again"}

	case errors.Is(err, ErrForbiddenRole):
		return Failure{http.StatusForbidden, CodeForbiddenRole,
			"this is not available on your account"}

	case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrNoSuchParent), errors.Is(err, ErrOriginRefused):
		return Failure{http.StatusBadRequest, CodeInvalidRequest,
			"that request was not accepted"}
	}

	return Failure{http.StatusInternalServerError, CodeInternalError,
		"something went wrong on our side"}
}

// envelope is the one failure shape the whole api answers with.
type envelope struct {
	Error envelopeBody `json:"error"`
}

// envelopeBody carries the code the client switches on and prose no client
// reads. No identifier, no name, no internal detail, ever.
type envelopeBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteFailure answers one request with the envelope.
//
// Note:
//   - no-store, because a refusal is per request and per parent. A cached 401
//     served to the next person is a session that ends for no reason.
//
// Param:
// response - http.ResponseWriter (the response being written)
// failure - Failure (the status, code, and wording)
func WriteFailure(response http.ResponseWriter, failure Failure) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(failure.Status)

	// The body is two short strings this package built, so a failed encode is
	// not a case that can happen. Ignoring it here beats answering a refusal
	// with a second refusal about the first one.
	_ = json.NewEncoder(response).Encode(envelope{
		Error: envelopeBody{Code: failure.Code, Message: failure.Message},
	})
}

// Deny answers one request with whatever that failure looks like on the wire.
func Deny(response http.ResponseWriter, err error) {
	WriteFailure(response, FailureFor(err))
}
