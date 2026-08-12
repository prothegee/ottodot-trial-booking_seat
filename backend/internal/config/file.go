package config

import (
    "encoding/json"
    "errors"
    "fmt"
    "io/fs"
    "os"
    "strconv"
    "strings"
)

// DefaultFilePath is where both processes look for their settings, relative to
// wherever they were started from. It is a path rather than an embedded default
// so one stack can run two configurations without a rebuild.
const DefaultFilePath = "config.json"

// The file is a flat object keyed by the same names the environment uses, so
// there is one vocabulary to learn instead of two, and a value can be moved
// between the two sources without being renamed.
//
// Values may be written in their natural json type. A port is a number, a flag
// is a boolean, and the origin list is an array of strings. Everything is turned
// into the same text the environment would have carried, which is what lets one
// parser serve both sources.
//
// An empty string, an empty array, and a missing key all mean the same thing:
// this setting is not stated here, so whatever comes next decides it.

// LoadFromFile reads the settings file, then the environment, then applies the
// defaults and validates the result.
//
// Note:
//   - the file wins. An environment variable fills in what the file leaves out
//     rather than overriding what it states, so what a reviewer reads in
//     config.json is what the process is running with.
//   - a missing file is not a failure. The process falls back to the
//     environment, which is what keeps a test and a bare `go run` working with
//     no file on disk at all.
//
// Param:
// path - string (where the settings file lives, usually DefaultFilePath)
//
// Return:
//   - the loaded configuration when every value is usable
//   - a zero configuration and an error when the file is unreadable, is not
//     json, or holds a value no setting can use
func LoadFromFile(path string) (Config, error) {
    fromFile, err := readSettingsFile(path)
    if err != nil {
        return Config{}, err
    }

    return Load(chainLookup(fromFile, os.LookupEnv))
}

// FileIsPresent reports whether a settings file is there to be read.
//
// It exists for the startup log line. A process that quietly fell back to the
// environment looks identical to one that read a file, and telling those two
// apart is the first question asked when a setting is not what somebody expects.
func FileIsPresent(path string) bool {
    _, err := os.Stat(path)

    return err == nil
}

// chainLookup asks the first source, then the second.
//
// A value the first source states, even a deliberately empty one, ends the
// search. That is what makes the file authoritative rather than merely
// consulted.
func chainLookup(first LookupFunc, second LookupFunc) LookupFunc {
    return func(key string) (string, bool) {
        if value, found := first(key); found {
            return value, true
        }

        return second(key)
    }
}

// readSettingsFile turns the file into a lookup.
//
// A file that is not there gives a lookup that finds nothing, so the caller
// needs no separate path for the missing case.
func readSettingsFile(path string) (LookupFunc, error) {
    raw, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return func(string) (string, bool) { return "", false }, nil
        }

        return nil, fmt.Errorf("the settings file %q could not be read: %w", path, err)
    }

    var stated map[string]any

    decoder := json.NewDecoder(strings.NewReader(string(raw)))
    decoder.UseNumber()

    if err := decoder.Decode(&stated); err != nil {
        return nil, fmt.Errorf("the settings file %q is not a json object: %w", path, err)
    }

    values := make(map[string]string, len(stated))

    for key, value := range stated {
        text, usable := settingText(value)
        if !usable {
            return nil, fmt.Errorf(
                "the settings file %q gives %s a value that is not text, a number, a flag, or a list of those",
                path, key)
        }

        values[key] = text
    }

    return func(key string) (string, bool) {
        value, found := values[key]

        return value, found
    }, nil
}

// settingText renders one json value as the text the environment would carry.
//
// A list becomes a comma separated string, which is how a multi valued setting
// travels in an environment variable, so both sources reach the parser looking
// the same.
//
// Return:
//   - the text and true for a string, a number, a boolean, or a list of those
//   - an empty string and false for anything else, including a nested object
func settingText(value any) (string, bool) {
    switch typed := value.(type) {
    case nil:
        return "", true

    case string:
        return typed, true

    case bool:
        return strconv.FormatBool(typed), true

    case json.Number:
        return typed.String(), true

    case []any:
        return listText(typed)
    }

    return "", false
}

// listText joins a json array into one comma separated value.
func listText(values []any) (string, bool) {
    parts := make([]string, 0, len(values))

    for _, value := range values {
        text, usable := settingText(value)
        if !usable {
            return "", false
        }

        if strings.TrimSpace(text) == "" {
            continue
        }

        parts = append(parts, strings.TrimSpace(text))
    }

    return strings.Join(parts, ","), true
}
