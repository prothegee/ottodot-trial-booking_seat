package cache

import "strings"

// keyPrefix namespaces everything this package writes.
//
// Redis is shared with the rate limiter and the token denylist, so a prefix is
// what stops a class list and a token bucket colliding on a name somebody chose
// twice.
const keyPrefix = "cache:"

// classListKey is the whole catalogue with its seat counts. There is one, so it
// has no identifier in it.
const classListKey = keyPrefix + "classes"

// ClassListKey names the cached class list.
func ClassListKey() string {
    return classListKey
}

// ClassKey names one cached class.
//
// Param:
// classID - string (which class)
//
// Return:
//   - the key, or an empty string when the id is empty, which every caller
//     treats as "do not cache this"
func ClassKey(classID string) string {
    if strings.TrimSpace(classID) == "" {
        return ""
    }

    return keyPrefix + "class:" + classID
}
