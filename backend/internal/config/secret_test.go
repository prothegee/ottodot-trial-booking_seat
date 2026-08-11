package config_test

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/config"
)

const plainSecret = "super-secret-signing-key-value"

func TestSecretNeverRendersItsValue(t *testing.T) {
    secret := config.Secret(plainSecret)

    cases := []struct {
        name     string
        rendered string
    }{
        {name: "unit: String", rendered: secret.String()},
        {name: "unit: GoString", rendered: secret.GoString()},
        {name: "unit: percent v", rendered: fmt.Sprintf("%v", secret)},
        {name: "unit: percent s", rendered: fmt.Sprintf("%s", secret)},
        {name: "unit: percent plus v", rendered: fmt.Sprintf("%+v", secret)},
        {name: "unit: percent hash v", rendered: fmt.Sprintf("%#v", secret)},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if strings.Contains(testCase.rendered, plainSecret) {
                t.Fatalf("the value leaked into %q", testCase.rendered)
            }

            if testCase.rendered != "[redacted]" {
                t.Fatalf("expected the mask, got %q", testCase.rendered)
            }
        })
    }
}

func TestSecretInsideAStructDoesNotLeak(t *testing.T) {
    // The accident this type exists for: a whole settings struct printed while
    // somebody is debugging something else entirely.
    holder := struct {
        Name  string
        Token config.Secret
    }{
        Name:  "auth",
        Token: config.Secret(plainSecret),
    }

    for _, verb := range []string{"%v", "%+v", "%#v"} {
        rendered := fmt.Sprintf(verb, holder)

        if strings.Contains(rendered, plainSecret) {
            t.Fatalf("the value leaked through %s: %q", verb, rendered)
        }
    }
}

func TestSecretMarshalsAsTheMask(t *testing.T) {
    encoded, err := json.Marshal(config.Secret(plainSecret))
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    if string(encoded) != `"[redacted]"` {
        t.Fatalf("expected the mask, got %s", encoded)
    }
}

func TestSecretRevealReturnsTheValue(t *testing.T) {
    if config.Secret(plainSecret).Reveal() != plainSecret {
        t.Fatal("Reveal must return the real value, it is the only way to read it")
    }
}

func TestSecretEdgeCases(t *testing.T) {
    t.Run("edge: an empty secret says unset rather than redacted", func(t *testing.T) {
        empty := config.Secret("")

        if empty.String() != "[unset]" {
            t.Fatalf("expected [unset], got %q", empty.String())
        }

        if !empty.IsEmpty() {
            t.Fatal("an empty secret must report itself as empty")
        }
    })

    t.Run("edge: a single space is a set value, not an empty one", func(t *testing.T) {
        spaced := config.Secret(" ")

        if spaced.IsEmpty() {
            t.Fatal("a space is a value, only the empty string is unset")
        }

        if spaced.String() != "[redacted]" {
            t.Fatalf("expected the mask, got %q", spaced.String())
        }
    })
}
