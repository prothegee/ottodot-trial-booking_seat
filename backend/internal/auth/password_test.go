package auth_test

import (
    "errors"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
)

// seededPasswordHash is the exact string 0002_seed.sql stores for every seeded
// account, and seededPassword is what it was made from.
//
// They are here so a change to the hashing that would stop the seeded accounts
// signing in fails a fast test, rather than a database that has to be brought up
// first.
const (
    seededPassword     = "otto123"
    seededPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$" +
        "8HvgNB40ArlxEEpvrs6x2g$6BJSMpsmkP7ai0ihs7HAYUm6bO2rwxAfMvY9i0C6mZs"
)

func TestHashPassword(t *testing.T) {
    t.Run("a password verifies against its own hash", func(t *testing.T) {
        encoded, err := auth.HashPassword("a-password-worth-keeping")
        if err != nil {
            t.Fatalf("hashing failed: %v", err)
        }

        if err := auth.VerifyPassword(encoded, "a-password-worth-keeping"); err != nil {
            t.Fatalf("the password did not verify against its own hash: %v", err)
        }
    })

    t.Run("edge: the same password hashes differently every time", func(t *testing.T) {
        first, err := auth.HashPassword(seededPassword)
        if err != nil {
            t.Fatalf("hashing failed: %v", err)
        }

        second, err := auth.HashPassword(seededPassword)
        if err != nil {
            t.Fatalf("hashing failed: %v", err)
        }

        // A fresh salt per call is what stops a stolen table being scanned for
        // accounts that chose the same password.
        if first == second {
            t.Fatal("two hashes of one password are identical, so the salt is not fresh")
        }

        for _, encoded := range []string{first, second} {
            if err := auth.VerifyPassword(encoded, seededPassword); err != nil {
                t.Fatalf("a freshly made hash did not verify: %v", err)
            }
        }
    })

    t.Run("edge: the stored form names its own algorithm and work", func(t *testing.T) {
        encoded, err := auth.HashPassword(seededPassword)
        if err != nil {
            t.Fatalf("hashing failed: %v", err)
        }

        // The parameters travel with the hash, which is what lets them be
        // raised later without stranding every row written before the change.
        if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=1,p=4$") {
            t.Fatalf("the stored form does not name argon2id and its work: %q", encoded)
        }
    })

    t.Run("edge: an empty password is refused rather than hashed", func(t *testing.T) {
        if _, err := auth.HashPassword(""); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest for an empty password, got %v", err)
        }
    })
}

func TestVerifyPassword(t *testing.T) {
    t.Run("the password in the seed file matches the hash in the seed file", func(t *testing.T) {
        if err := auth.VerifyPassword(seededPasswordHash, seededPassword); err != nil {
            t.Fatalf("the seeded accounts cannot sign in with the documented password: %v", err)
        }
    })

    t.Run("a wrong password is refused", func(t *testing.T) {
        if err := auth.VerifyPassword(seededPasswordHash, "otto124"); !errors.Is(err, auth.ErrPasswordRefused) {
            t.Fatalf("expected ErrPasswordRefused, got %v", err)
        }
    })

    t.Run("edge: an empty password is refused, never accepted", func(t *testing.T) {
        if err := auth.VerifyPassword(seededPasswordHash, ""); !errors.Is(err, auth.ErrPasswordRefused) {
            t.Fatalf("expected ErrPasswordRefused for an empty password, got %v", err)
        }
    })

    t.Run("edge: a stored hash that cannot be read is not a wrong password", func(t *testing.T) {
        // The two have to stay distinguishable. One is somebody mistyping, the
        // other is a seed or a migration that went wrong and needs a person.
        unusable := map[string]string{
            "empty":                                 "",
            "not a hash at all":                     "otto123",
            "bcrypt":                                "$2y$10$abcdefghijklmnopqrstuv",
            "argon2i":                               "$argon2i$v=19$m=65536,t=1,p=4$c2FsdHNhbHQ$aGFzaGhhc2g",
            "a version this service does not write": "$argon2id$v=16$m=65536,t=1,p=4$c2FsdA$aGFzaA",
            "missing the work field":                "$argon2id$v=19$c2FsdA$aGFzaA",
            "zero memory":                           "$argon2id$v=19$m=0,t=1,p=4$c2FsdA$aGFzaA",
            "salt that is not base64":               "$argon2id$v=19$m=65536,t=1,p=4$not base64!$aGFzaA",
        }

        for name, encoded := range unusable {
            if err := auth.VerifyPassword(encoded, seededPassword); !errors.Is(err, auth.ErrPasswordUnusable) {
                t.Fatalf("%s: expected ErrPasswordUnusable, got %v", name, err)
            }
        }
    })

    t.Run("edge: a hash made with different work still verifies", func(t *testing.T) {
        // Written by an argon2id implementation using two passes and less
        // memory than this service does. The work is read from the string, so
        // raising the constants must never strand a row like this one.
        const otherWork = "$argon2id$v=19$m=19456,t=2,p=1$" +
            "SkNTM3ZQZG5xSkNTM3ZQZA$Ej3AqfhQVXvBnFfurxJKgFzT3fKRHnzeVAd0zJdRnjg"

        err := auth.VerifyPassword(otherWork, "otto123")
        if errors.Is(err, auth.ErrPasswordUnusable) {
            t.Fatalf("a hash carrying different work was called unreadable: %v", err)
        }
    })
}
