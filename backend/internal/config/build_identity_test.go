package config_test

import (
    "os"
    "regexp"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/config"
)

// The backend version number is written down in two committed files, because
// the two things that run this code are configured differently: a process
// started from source reads config.json, and a container is configured by
// compose, which deliberately does not mount that file.
//
// Two places means drift, and drift here is silent: the api would answer one
// version and the image would be tagged another, with nothing failing. These
// cases are what makes that a broken build instead.
//
// The commit is not written down anywhere, so what is pinned here instead is
// that each way of starting the backend still asks the toolchain for it.

const (
    templatePath = "../../config.json.template"
    composePath  = "../../compose.yml"
    debugPath    = "../../scripts/debug.sh"
)

// composeBuildVersions finds every default compose falls back to for the
// version, in the build arguments, the environment, and the image tags.
var composeBuildVersions = regexp.MustCompile(`\$\{BUILD_VERSION:-([^}]*)\}`)

func TestTheCommittedVersionIsStated(t *testing.T) {
    settings, err := config.LoadFromFile(templatePath)
    if err != nil {
        t.Fatalf("the committed template does not load: %v", err)
    }

    t.Run("behaviour: the template names a version", func(t *testing.T) {
        // Without this the api answers unknown, which is what a reviewer sees
        // on the status screen and cannot do anything about.
        if settings.Build.Version == "" {
            t.Fatal("the template states no BUILD_VERSION, so a run from source cannot name itself")
        }
    })

    t.Run("edge: the version is a number and not a word", func(t *testing.T) {
        // "dev" was the old default and it is not an answer. It says nothing
        // about which build is running, which is the only question this field
        // is asked.
        if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(settings.Build.Version) {
            t.Fatalf("expected a version number, got %q", settings.Build.Version)
        }
    })

    t.Run("edge: the commit is left to the build", func(t *testing.T) {
        // A commit written down in a committed file is stale the moment it is
        // written, and it would override the one the toolchain recorded.
        if settings.Build.Commit != "" {
            t.Fatalf("the template pins a commit, got %q", settings.Build.Commit)
        }
    })
}

func TestTheContainerAndTheFileAgreeOnTheVersion(t *testing.T) {
    settings, err := config.LoadFromFile(templatePath)
    if err != nil {
        t.Fatalf("the committed template does not load: %v", err)
    }

    composeFile, err := os.ReadFile(composePath)
    if err != nil {
        t.Fatalf("the compose file cannot be read: %v", err)
    }

    found := composeBuildVersions.FindAllStringSubmatch(string(composeFile), -1)

    t.Run("integration: compose still falls back to a version at all", func(t *testing.T) {
        if len(found) == 0 {
            t.Fatal("compose names no BUILD_VERSION default, so an image tag would be empty")
        }
    })

    t.Run("integration: every compose default is the version the template states", func(t *testing.T) {
        for _, match := range found {
            if match[1] != settings.Build.Version {
                t.Fatalf("compose falls back to %q and config.json.template states %q",
                    match[1], settings.Build.Version)
            }
        }
    })
}

func TestTheFromSourceRunnerAsksForTheRevision(t *testing.T) {
    runner, err := os.ReadFile(debugPath)
    if err != nil {
        t.Fatalf("the from-source runner cannot be read: %v", err)
    }

    t.Run("integration: the process is started with the revision recorded", func(t *testing.T) {
        // `go build` records the revision without being asked and `go run` does
        // not, so the flag is the whole difference between /version naming this
        // checkout and /version answering unknown.
        if !strings.Contains(string(runner), "go run -buildvcs=true") {
            t.Fatal("scripts/debug.sh runs go run without -buildvcs=true, so the process cannot name its commit")
        }
    })
}
