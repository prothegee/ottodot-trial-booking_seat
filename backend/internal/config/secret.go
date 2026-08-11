package config

// Secret is a configuration value that must never reach a log line, an error
// message, or a printed struct.
//
// The type exists because the usual accident is not a deliberate print. It is a
// struct dumped with %v while somebody is debugging something else, which is
// exactly the moment a secret is least likely to be noticed. Reveal is the only
// way to read the real value, so every read of it is visible in a review.
type Secret string

// String satisfies fmt.Stringer, so %v and %s render the mask instead of the
// value.
func (secret Secret) String() string {
    if secret == "" {
        return "[unset]"
    }

    return "[redacted]"
}

// GoString satisfies fmt.GoStringer, so %#v renders the mask as well.
func (secret Secret) GoString() string {
    return secret.String()
}

// MarshalJSON keeps the value out of anything serialized, including a
// diagnostic endpoint that marshals a configuration struct.
func (secret Secret) MarshalJSON() ([]byte, error) {
    return []byte(`"` + secret.String() + `"`), nil
}

// Reveal returns the real value. Call it at the point of use, never to build a
// message.
func (secret Secret) Reveal() string {
    return string(secret)
}

// IsEmpty reports whether the secret was never set.
func (secret Secret) IsEmpty() bool {
    return secret == ""
}
