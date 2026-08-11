package auth_test

import (
    "context"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
)

// memoryDirectoryFixture points the shared contract at the fake, seeded with
// the same two accounts the migration seeds.
type memoryDirectoryFixture struct {
    directory *auth.MemoryDirectory
}

func newMemoryDirectoryFixture(t *testing.T) directoryFixture {
    return &memoryDirectoryFixture{directory: seededDirectory()}
}

func (fixture *memoryDirectoryFixture) Directory() auth.Directory {
    return fixture.directory
}

// seededDirectory builds the fake with the two accounts the contract names.
//
// It is shared by every test in this package that needs somebody to sign in as,
// so there is one place where the seeded facts live.
func seededDirectory() *auth.MemoryDirectory {
    directory := auth.NewMemoryDirectory()

    directory.Add(
        contractParentEmail,
        auth.Parent{ID: contractParentID, DisplayName: contractParentName, Role: auth.RoleParent},
        []auth.Child{{ID: contractChildID, FullName: contractChildName, GradeLevel: 5}})

    directory.Add(
        contractLonelyParentEmail,
        auth.Parent{ID: contractLonelyParentID, DisplayName: "Chandra Wijaya", Role: auth.RoleParent},
        nil)

    directory.Add(
        "ops.admin@example.test",
        auth.Parent{ID: adminParentID, DisplayName: "Ops Admin", Role: auth.RoleAdmin},
        nil)

    return directory
}

// adminParentID is the seeded operator account, which the role tests sign in
// as.
const adminParentID = "0192a000-0000-7000-8000-000000000009"

func TestTheMemoryDirectoryHonoursTheContract(t *testing.T) {
    runDirectoryContract(t, newMemoryDirectoryFixture)
}

func TestTheMemoryDirectoryHoldsNothingItShouldNot(t *testing.T) {
    t.Run("edge: a parent read back carries no email", func(t *testing.T) {
        // The type has no field for one, so this cannot fail today. It is
        // asserted because the change that would break it is somebody adding a
        // convenient field, and an email that is never loaded is an email that
        // can never be logged.
        found, err := seededDirectory().ParentByEmail(context.Background(), contractParentEmail)
        if err != nil {
            t.Fatalf("cannot look the parent up: %v", err)
        }

        if found.DisplayName == contractParentEmail {
            t.Fatal("the display name is the email address")
        }
    })

    t.Run("edge: the caller's child slice is copied, so it cannot be rewritten later", func(t *testing.T) {
        directory := auth.NewMemoryDirectory()

        children := []auth.Child{{ID: contractChildID, FullName: contractChildName, GradeLevel: 5}}

        directory.Add(
            contractParentEmail,
            auth.Parent{ID: contractParentID, DisplayName: contractParentName, Role: auth.RoleParent},
            children)

        children[0].FullName = "rewritten after the fact"

        account, err := directory.Account(context.Background(), contractParentID)
        if err != nil {
            t.Fatalf("cannot read the account: %v", err)
        }

        if account.Children[0].FullName != contractChildName {
            t.Fatal("the stored child followed the caller's slice")
        }
    })

    t.Run("edge: the answer is a copy, so a caller cannot rewrite the directory", func(t *testing.T) {
        directory := seededDirectory()

        account, err := directory.Account(context.Background(), contractParentID)
        if err != nil {
            t.Fatalf("cannot read the account: %v", err)
        }

        account.Children[0].FullName = "rewritten by the caller"

        again, err := directory.Account(context.Background(), contractParentID)
        if err != nil {
            t.Fatalf("cannot read the account again: %v", err)
        }

        if again.Children[0].FullName != contractChildName {
            t.Fatal("the directory followed a caller's edit of the answer it handed out")
        }
    })
}
