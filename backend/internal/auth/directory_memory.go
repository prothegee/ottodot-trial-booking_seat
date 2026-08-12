package auth

import (
    "context"
    "strings"
    "sync"
)

// MemoryDirectory is the fake, holding the seeded accounts a test needs.
//
// It normalises the email the same way the sql does, because the one thing a
// directory fake can get wrong and still pass its own tests is matching on a
// rule the database does not use.
type MemoryDirectory struct {
    mutex sync.RWMutex

    credentialsByEmail map[string]Credential
    parentsByID        map[string]Parent
    childrenByID       map[string][]Child
}

// NewMemoryDirectory builds an empty directory.
func NewMemoryDirectory() *MemoryDirectory {
    return &MemoryDirectory{
        credentialsByEmail: make(map[string]Credential),
        parentsByID:        make(map[string]Parent),
        childrenByID:       make(map[string][]Child),
    }
}

// Add puts one account in the directory. Tests build their own seed with it.
//
// Note:
//   - the hash is passed in already made rather than hashed here. argon2 is
//     deliberately slow, and a fixture that hashed on every Add would spend more
//     time seeding than testing.
//
// Param:
// email - string (the address sign in is by, matched case insensitively)
// passwordHash - string (the stored argon2id string, empty for an account that
// is not meant to be signed in to)
// parent - Parent (who the account belongs to)
// children - []Child (the students on it, which may be none)
func (directory *MemoryDirectory) Add(
    email string,
    passwordHash string,
    parent Parent,
    children []Child,
) {
    directory.mutex.Lock()
    defer directory.mutex.Unlock()

    directory.credentialsByEmail[normaliseEmail(email)] = Credential{
        Parent:       parent,
        PasswordHash: passwordHash,
    }
    directory.parentsByID[parent.ID] = parent

    held := make([]Child, len(children))
    copy(held, children)

    directory.childrenByID[parent.ID] = held
}

// CredentialByEmail is the sign in lookup.
func (directory *MemoryDirectory) CredentialByEmail(ctx context.Context, email string) (Credential, error) {
    normalised := normaliseEmail(email)
    if normalised == "" {
        return Credential{}, ErrInvalidRequest
    }

    directory.mutex.RLock()
    defer directory.mutex.RUnlock()

    found, exists := directory.credentialsByEmail[normalised]
    if !exists {
        return Credential{}, ErrNoSuchParent
    }

    return found, nil
}

// Account is the session read.
func (directory *MemoryDirectory) Account(ctx context.Context, parentID string) (Account, error) {
    if parentID == "" {
        return Account{}, ErrInvalidRequest
    }

    directory.mutex.RLock()
    defer directory.mutex.RUnlock()

    found, exists := directory.parentsByID[parentID]
    if !exists {
        return Account{}, ErrNoSuchParent
    }

    held := directory.childrenByID[parentID]

    children := make([]Child, len(held))
    copy(children, held)

    return Account{Parent: found, Children: children}, nil
}

// normaliseEmail trims and lowercases, which is what the sql lookup does.
//
// An address typed with a capital letter is the same account, and a sign in
// that fails on capitalisation is a support ticket rather than a security
// property.
func normaliseEmail(email string) string {
    return strings.ToLower(strings.TrimSpace(email))
}
