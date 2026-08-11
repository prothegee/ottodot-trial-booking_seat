package ratelimit

import "strings"

// keyPrefix namespaces everything this package writes.
//
// Redis is shared with the response cache and the token denylist, so a prefix is
// what stops a token bucket and a class list colliding on a name somebody chose
// twice.
const keyPrefix = "ratelimit:"

// SubjectKey names the bucket for one signed in parent.
//
// This is the bucket that matters. A token subject is a real account this
// service issued, so it cannot be changed by dialling a different address.
//
// Param:
// subject - string (the parent id from the access token)
//
// Return:
//   - the key, or an empty string when there is no subject, which every caller
//     treats as "there is nothing to count here"
func SubjectKey(subject string) string {
    if strings.TrimSpace(subject) == "" {
        return ""
    }

    return keyPrefix + "subject:" + subject
}

// AddressKey names the bucket for one caller address.
//
// It is the weaker of the two and it is here for what happens before a token
// exists: a flood at the sign in route has no subject to count against. An
// address is shared by everyone behind one office connection and changed at will
// by anyone with a pool of them, so nothing important rests on it alone.
//
// Param:
// address - string (the caller address, already stripped of its port)
//
// Return:
//   - the key, or an empty string when the address is unknown
func AddressKey(address string) string {
    if strings.TrimSpace(address) == "" {
        return ""
    }

    return keyPrefix + "address:" + address
}
