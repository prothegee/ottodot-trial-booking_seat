package payment

// MaxIdempotencyKeyLength bounds what this service will store and index.
//
// The column is text and would take far more. The bound exists because the key
// arrives in an `Idempotency-Key` header from a client, and an unbounded header
// written straight into a unique index is a way to make somebody else's request
// slow. 128 characters is longer than any identifier or hash a client would
// reasonably send.
const MaxIdempotencyKeyLength = 128

// ValidateIdempotencyKey refuses a key before any row is written.
//
// Note:
//   - the key travels in a header, so it has to be one printable token. Space,
//     tab, newline, and every control character are refused, which also removes
//     the only shape that could split a header line.
//
// Param:
// key - string (the value the client sent in the Idempotency-Key header)
//
// Return:
//   - nil when the key can be stored and matched
//   - ErrInvalidIdempotencyKey when it is empty, longer than the bound, or
//     carries a character a header cannot hold
func ValidateIdempotencyKey(key string) error {
	if key == "" || len(key) > MaxIdempotencyKeyLength {
		return ErrInvalidIdempotencyKey
	}

	for i := 0; i < len(key); i++ {
		if key[i] <= ' ' || key[i] > '~' {
			return ErrInvalidIdempotencyKey
		}
	}

	return nil
}
