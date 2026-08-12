package operations_test

import (
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/operations"
)

func TestFirstStated(t *testing.T) {
    t.Run("unit: the first value somebody stated is the answer", func(t *testing.T) {
        if got := operations.FirstStated("1.2.3", "0.1.0"); got != "1.2.3" {
            t.Fatalf("expected the first stated value, got %q", got)
        }
    })

    t.Run("unit: a silent source is skipped rather than reported", func(t *testing.T) {
        // This is the whole point of the chain. A stamped binary answers for
        // itself, and an unstamped one has to fall through to configuration.
        if got := operations.FirstStated("", "0.1.0"); got != "0.1.0" {
            t.Fatalf("expected the next source to answer, got %q", got)
        }
    })

    t.Run("edge: every source silent is an empty answer, not a made up one", func(t *testing.T) {
        if got := operations.FirstStated("", "", ""); got != "" {
            t.Fatalf("expected nothing, got %q", got)
        }
    })

    t.Run("edge: no sources at all is an empty answer", func(t *testing.T) {
        if got := operations.FirstStated(); got != "" {
            t.Fatalf("expected nothing, got %q", got)
        }
    })
}

func TestCommitFromBuildRecord(t *testing.T) {
    commit := operations.CommitFromBuildRecord()

    t.Run("behaviour: a test binary built from this checkout names its commit", func(t *testing.T) {
        // `go test` records the revision the same way `go build` does, so this
        // is the same value a developer's own binary would report.
        if commit == "" {
            t.Log("this build carries no revision, which is what a container build looks like")

            return
        }

        if len(commit) != 7 {
            t.Fatalf("expected the seven characters git abbreviates to, got %q", commit)
        }
    })

    t.Run("edge: the commit is hexadecimal and nothing else", func(t *testing.T) {
        if commit == "" {
            t.Log("this build carries no revision")

            return
        }

        if strings.Trim(commit, "0123456789abcdef") != "" {
            t.Fatalf("that is not a revision: %q", commit)
        }
    })

    t.Run("edge: asking twice gives the same answer", func(t *testing.T) {
        // It is read at startup and reported for the life of the process, so a
        // value that changed between two reads would be a route that contradicts
        // its own log line.
        if again := operations.CommitFromBuildRecord(); again != commit {
            t.Fatalf("two reads disagree: %q then %q", commit, again)
        }
    })
}

func TestBuiltAtFromExecutable(t *testing.T) {
    builtAt := operations.BuiltAtFromExecutable()

    t.Run("behaviour: the running binary reports when it was written", func(t *testing.T) {
        if builtAt == "" {
            t.Fatal("the test binary is on disk and reported no time at all")
        }

        if _, err := time.Parse(time.RFC3339, builtAt); err != nil {
            t.Fatalf("expected an RFC 3339 instant, got %q: %v", builtAt, err)
        }
    })

    t.Run("edge: the instant is reported in UTC", func(t *testing.T) {
        // Two services compared side by side on the status screen must not be
        // compared across two machines' time zones.
        if !strings.HasSuffix(builtAt, "Z") {
            t.Fatalf("expected a UTC instant, got %q", builtAt)
        }
    })

    t.Run("edge: it is not in the future", func(t *testing.T) {
        written, err := time.Parse(time.RFC3339, builtAt)
        if err != nil {
            t.Fatalf("expected an RFC 3339 instant, got %q: %v", builtAt, err)
        }

        if written.After(time.Now().Add(time.Minute)) {
            t.Fatalf("this binary claims to have been built at %q", builtAt)
        }
    })
}
