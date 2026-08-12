package auth

import (
    "crypto/rand"
    "crypto/subtle"
    "encoding/base64"
    "errors"
    "fmt"
    "strconv"
    "strings"

    "golang.org/x/crypto/argon2"
)

// Passwords are stored as an argon2id hash and never in any other form.
//
// argon2id rather than bcrypt or a plain sha: it is deliberately slow and
// deliberately memory hungry, which is what makes guessing a stolen table
// expensive rather than a weekend of graphics card time.
//
// The parameters travel inside the stored string rather than living as
// constants here. Raising them later is then a decision about new passwords,
// and every password stored under the old numbers still verifies, because each
// row says how it was made.

// The work each new hash is made with.
//
// 64 megabytes and one pass is the configuration the argon2 authors give for
// argon2id when memory is available. The parallelism is fixed rather than taken
// from the processor count, because a hash made on an eight core machine has to
// verify on a two core one.
const (
    hashMemoryKiB  = 64 * 1024
    hashIterations = 1
    hashThreads    = 4
    hashSaltLength = 16
    hashKeyLength  = 32
)

// hashEncodingVersion is the argon2 version the encoded form names. A stored
// hash naming any other version is refused rather than guessed at.
const hashEncodingVersion = argon2.Version

// hashAlgorithmName is the only algorithm this service accepts. argon2i and
// argon2d are different functions, and a stored hash naming one of them was not
// made by this service.
const hashAlgorithmName = "argon2id"

// ErrPasswordRefused means the password did not match the stored hash.
//
// It is deliberately the same answer for a wrong password and an unknown
// account. See LogIn for why.
var ErrPasswordRefused = errors.New("auth: the password does not match")

// ErrPasswordUnusable means the stored hash cannot be read, so nothing can be
// decided from it.
//
// This is a broken row rather than a wrong password, and the two must not share
// an error: one is somebody mistyping, the other is a seed or a migration that
// went wrong and needs a person.
var ErrPasswordUnusable = errors.New("auth: the stored password hash cannot be read")

// HashPassword turns a password into the string a row stores.
//
// The salt is fresh for every call, so two accounts choosing the same password
// store two different hashes and a stolen table cannot be scanned for repeats.
//
// Param:
// password - string (what the parent typed, exactly, with no trimming)
//
// Return:
//   - the encoded hash, in the standard argon2 form that names its own
//     parameters
//   - ErrInvalidRequest for an empty password
//   - an error when the system has no randomness to draw a salt from
func HashPassword(password string) (string, error) {
    if password == "" {
        return "", ErrInvalidRequest
    }

    salt := make([]byte, hashSaltLength)

    if _, err := rand.Read(salt); err != nil {
        return "", fmt.Errorf("a password salt could not be drawn: %w", err)
    }

    key := argon2.IDKey(
        []byte(password), salt, hashIterations, hashMemoryKiB, hashThreads, hashKeyLength)

    return fmt.Sprintf(
        "$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
        hashAlgorithmName,
        hashEncodingVersion,
        hashMemoryKiB,
        hashIterations,
        hashThreads,
        base64.RawStdEncoding.EncodeToString(salt),
        base64.RawStdEncoding.EncodeToString(key),
    ), nil
}

// VerifyPassword checks a password against a stored hash.
//
// Note:
//   - the comparison is constant time. A byte by byte comparison that returned
//     early would leak how much of a guess was right through how long the answer
//     took.
//   - the work is read from the stored string, not from the constants above, so
//     a row written before those numbers changed still verifies.
//
// Param:
// encoded - string (the stored hash, as HashPassword wrote it)
// password - string (what the parent typed)
//
// Return:
//   - nil when the password matches
//   - ErrPasswordRefused when it does not
//   - ErrPasswordUnusable when the stored string is not a hash this service
//     could have written
func VerifyPassword(encoded string, password string) error {
    stored, err := parseEncodedHash(encoded)
    if err != nil {
        return err
    }

    candidate := argon2.IDKey(
        []byte(password),
        stored.salt,
        stored.iterations,
        stored.memoryKiB,
        stored.threads,
        uint32(len(stored.key)),
    )

    if subtle.ConstantTimeCompare(candidate, stored.key) != 1 {
        return ErrPasswordRefused
    }

    return nil
}

// workSink is where the deliberately wasted hash goes.
//
// It is a package variable rather than a discard, so no compiler can decide the
// call had no effect and remove the very cost it exists to pay.
var workSink []byte

// decoySalt is the salt the wasted hash uses. Its value does not matter and it
// is never stored, only the time it takes to use it.
var decoySalt = make([]byte, hashSaltLength)

// spendPasswordWork does one hash and throws it away.
//
// It is called on the path where no account was found, so that path costs what
// a real verification costs. Without it, an address nobody holds is answered
// noticeably faster than one that exists with a wrong password, and that
// difference is readable from outside.
func spendPasswordWork(password string) {
    workSink = argon2.IDKey(
        []byte(password), decoySalt, hashIterations, hashMemoryKiB, hashThreads, hashKeyLength)
}

// encodedHash is a stored hash taken apart into the pieces a verification needs.
type encodedHash struct {
    memoryKiB  uint32
    iterations uint32
    threads    uint8
    salt       []byte
    key        []byte
}

// parseEncodedHash reads the standard argon2 encoding.
//
// The form is `$argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>`, which is what
// every argon2 implementation writes, so a row here can be read by something
// that is not this service.
func parseEncodedHash(encoded string) (encodedHash, error) {
    fields := strings.Split(encoded, "$")
    if len(fields) != 6 || fields[0] != "" {
        return encodedHash{}, ErrPasswordUnusable
    }

    if fields[1] != hashAlgorithmName {
        return encodedHash{}, ErrPasswordUnusable
    }

    var version int

    if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil || version != hashEncodingVersion {
        return encodedHash{}, ErrPasswordUnusable
    }

    parsed, err := parseHashWork(fields[3])
    if err != nil {
        return encodedHash{}, err
    }

    parsed.salt, err = base64.RawStdEncoding.DecodeString(fields[4])
    if err != nil || len(parsed.salt) == 0 {
        return encodedHash{}, ErrPasswordUnusable
    }

    parsed.key, err = base64.RawStdEncoding.DecodeString(fields[5])
    if err != nil || len(parsed.key) == 0 {
        return encodedHash{}, ErrPasswordUnusable
    }

    return parsed, nil
}

// parseHashWork reads the `m=,t=,p=` field.
//
// Each number is checked for being usable rather than merely parseable. A zero
// anywhere here would make argon2 panic, and a stored row must never be able to
// stop the process that reads it.
func parseHashWork(field string) (encodedHash, error) {
    parts := strings.Split(field, ",")
    if len(parts) != 3 {
        return encodedHash{}, ErrPasswordUnusable
    }

    memory, memoryErr := parseLabelled(parts[0], "m=", 32)
    iterations, iterationsErr := parseLabelled(parts[1], "t=", 32)
    threads, threadsErr := parseLabelled(parts[2], "p=", 8)

    if memoryErr != nil || iterationsErr != nil || threadsErr != nil {
        return encodedHash{}, ErrPasswordUnusable
    }

    return encodedHash{
        memoryKiB:  uint32(memory),
        iterations: uint32(iterations),
        threads:    uint8(threads),
    }, nil
}

// parseLabelled reads one `label=number` pair and refuses a zero.
func parseLabelled(field string, label string, bitSize int) (uint64, error) {
    if !strings.HasPrefix(field, label) {
        return 0, ErrPasswordUnusable
    }

    value, err := strconv.ParseUint(strings.TrimPrefix(field, label), 10, bitSize)
    if err != nil || value == 0 {
        return 0, ErrPasswordUnusable
    }

    return value, nil
}
