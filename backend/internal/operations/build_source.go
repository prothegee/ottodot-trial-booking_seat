package operations

import (
    "os"
    "runtime/debug"
    "time"
)

// Where a build identity comes from when the linker did not stamp one.
//
// A release binary is stamped with -X and answers for itself. Everything else,
// a `go run` during development and a container built from a source copy, has to
// work its identity out, and the two functions here are the sources that need
// nothing written down anywhere: the record the Go toolchain embeds in every
// binary it builds from a checkout, and the binary's own file.

// shortCommitLength is how much of a revision is reported. Seven characters is
// what git itself abbreviates to and what the client's footer shows, so the two
// halves of the system can be compared by eye on the status screen.
const shortCommitLength = 7

// FirstStated is the first of these values somebody actually stated.
//
// It is what puts the sources of a build identity in order. An empty value means
// nobody answered, so the next source is asked, and the last one in the list is
// the fallback.
//
// Param:
// values - ...string (the sources, most authoritative first)
//
// Return:
//   - the first value that is not empty
//   - an empty string when every source was silent
func FirstStated(values ...string) string {
    for _, value := range values {
        if value != "" {
            return value
        }
    }

    return ""
}

// CommitFromBuildRecord is the revision this binary was built from, as the Go
// toolchain recorded it.
//
// Note:
//   - the toolchain writes this without being asked and without a build flag, so
//     it cannot disagree with the source that produced the binary.
//   - it is absent for a build whose context carries the source without the
//     repository, which is what a container build is. There the linker or the
//     configuration answers instead.
//
// Return:
//   - the short commit, abbreviated the way git abbreviates it
//   - an empty string when no revision was recorded
func CommitFromBuildRecord() string {
    revision := buildRecordValue("vcs.revision")

    if len(revision) > shortCommitLength {
        return revision[:shortCommitLength]
    }

    return revision
}

// BuiltAtFromExecutable is when the running binary was written, read from the
// binary itself.
//
// Note:
//   - this is the file's own timestamp, so it is the moment the linker produced
//     it. That holds for a `go run` and for a container alike, because copying a
//     file into an image keeps its time.
//   - it is reported in UTC, so two services compared side by side are not being
//     compared across two machines' time zones.
//
// Return:
//   - an RFC 3339 instant
//   - an empty string when the executable cannot be found or read, which is not
//     worth failing a startup over
func BuiltAtFromExecutable() string {
    path, err := os.Executable()
    if err != nil {
        return ""
    }

    details, err := os.Stat(path)
    if err != nil {
        return ""
    }

    return details.ModTime().UTC().Format(time.RFC3339)
}

// buildRecordValue reads one key out of the record the toolchain embedded.
func buildRecordValue(key string) string {
    info, ok := debug.ReadBuildInfo()
    if !ok {
        return ""
    }

    for _, setting := range info.Settings {
        if setting.Key == key {
            return setting.Value
        }
    }

    return ""
}
