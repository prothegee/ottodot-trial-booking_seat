package config_test

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/config"
)

// writeSettingsFile puts a settings file in a directory only this test uses.
func writeSettingsFile(t *testing.T, contents string) string {
    t.Helper()

    path := filepath.Join(t.TempDir(), "config.json")

    if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
        t.Fatalf("cannot write the settings file: %v", err)
    }

    return path
}

func TestLoadFromFile(t *testing.T) {
    t.Run("integration: every json type reaches the setting it names", func(t *testing.T) {
        path := writeSettingsFile(t, `{
            "APP_ENV": "development",
            "API_PORT": 9100,
            "WORKER_COUNT": 3,
            "COOKIE_SECURE": false,
            "ACCESS_TOKEN_TTL": "20m",
            "ALLOWED_ORIGINS": ["http://127.0.0.1:9001", "http://localhost:9001"]
        }`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Api.Port != 9100 {
            t.Fatalf("expected port 9100, got %d", settings.Api.Port)
        }

        if settings.Worker.Count != 3 {
            t.Fatalf("expected three workers, got %d", settings.Worker.Count)
        }

        if settings.Auth.CookieSecure {
            t.Fatal("a false in the file did not reach the setting")
        }

        if settings.Auth.AccessTokenTTL.Minutes() != 20 {
            t.Fatalf("expected twenty minutes, got %s", settings.Auth.AccessTokenTTL)
        }

        if len(settings.AllowedOrigins) != 2 {
            t.Fatalf("expected both origins, got %v", settings.AllowedOrigins)
        }
    })

    t.Run("the file wins over the environment", func(t *testing.T) {
        // The whole point of the file. What a reviewer reads in it is what the
        // process is running with, whatever the shell happens to carry.
        t.Setenv("API_PORT", "9500")

        path := writeSettingsFile(t, `{"API_PORT": 9100}`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Api.Port != 9100 {
            t.Fatalf("the environment overrode the file, got port %d", settings.Api.Port)
        }
    })

    t.Run("the environment fills in what the file leaves out", func(t *testing.T) {
        t.Setenv("API_PORT", "9500")

        path := writeSettingsFile(t, `{"WORKER_COUNT": 2}`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Api.Port != 9500 {
            t.Fatalf("expected the environment to decide the port, got %d", settings.Api.Port)
        }
    })

    t.Run("edge: a missing file is not a failure", func(t *testing.T) {
        // A fresh clone, a bare go run, and every test in this repository run
        // without one.
        settings, err := config.LoadFromFile(filepath.Join(t.TempDir(), "nothing-here.json"))
        if err != nil {
            t.Fatalf("a missing settings file was treated as a failure: %v", err)
        }

        if settings.Api.Port != 9000 {
            t.Fatalf("expected the defaults, got port %d", settings.Api.Port)
        }
    })

    t.Run("edge: a file that is not json names itself in the refusal", func(t *testing.T) {
        path := writeSettingsFile(t, `{"API_PORT": 9100`)

        _, err := config.LoadFromFile(path)
        if err == nil {
            t.Fatal("a truncated settings file loaded")
        }

        if !strings.Contains(err.Error(), path) {
            t.Fatalf("the refusal does not say which file, got: %v", err)
        }
    })

    t.Run("edge: a nested object is refused rather than flattened", func(t *testing.T) {
        // The file is flat by design, keyed the same way the environment is. A
        // nested shape is somebody expecting a different format, and guessing
        // at what they meant would be worse than saying so.
        path := writeSettingsFile(t, `{"api": {"port": 9100}}`)

        _, err := config.LoadFromFile(path)
        if err == nil {
            t.Fatal("a nested settings file loaded")
        }

        if !strings.Contains(err.Error(), "api") {
            t.Fatalf("the refusal does not name the key, got: %v", err)
        }
    })

    t.Run("edge: an empty value in the file falls through rather than blanking a setting", func(t *testing.T) {
        path := writeSettingsFile(t, `{"REDIS_ADDRESS": "", "ALLOWED_ORIGINS": []}`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Redis.Address != "127.0.0.1:6379" {
            t.Fatalf("an empty string blanked the setting instead of falling through, got %q",
                settings.Redis.Address)
        }

        if len(settings.AllowedOrigins) != len(config.DefaultAllowedOrigins()) {
            t.Fatalf("an empty list blanked the origins instead of falling through, got %v",
                settings.AllowedOrigins)
        }
    })
}

func TestTheSigningKeyComesFromTheFile(t *testing.T) {
    const statedKey = "a-signing-key-stated-in-the-settings-file"

    t.Run("integration: the key in the file is the key the service signs with", func(t *testing.T) {
        path := writeSettingsFile(t, `{"JWT_SECRET": "`+statedKey+`"}`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Auth.JWTSecret.Reveal() != statedKey {
            t.Fatal("the signing key did not come from the file")
        }
    })

    t.Run("behaviour: the file beats a key exported in the shell", func(t *testing.T) {
        t.Setenv("JWT_SECRET", "a-signing-key-exported-in-the-environment")

        path := writeSettingsFile(t, `{"JWT_SECRET": "`+statedKey+`"}`)

        settings, err := config.LoadFromFile(path)
        if err != nil {
            t.Fatalf("the settings file was refused: %v", err)
        }

        if settings.Auth.JWTSecret.Reveal() != statedKey {
            t.Fatal("the environment overrode the signing key stated in the file")
        }
    })

    t.Run("edge: a short key from the file is refused outside development", func(t *testing.T) {
        // The file is not a way around the length rule. It is read earlier than
        // the environment, not more trustingly.
        path := writeSettingsFile(t, `{"APP_ENV": "production", "JWT_SECRET": "short", "COOKIE_SECURE": true}`)

        _, err := config.LoadFromFile(path)
        if err == nil {
            t.Fatal("a short signing key from the file was accepted in production")
        }

        if !strings.Contains(err.Error(), "JWT_SECRET") {
            t.Fatalf("the refusal does not name the setting, got: %v", err)
        }

        // The key itself must not be echoed back, in the file case as in every
        // other one.
        if strings.Contains(err.Error(), "short") && !strings.Contains(err.Error(), "shorter") {
            t.Fatalf("the refusal repeats the key back: %v", err)
        }
    })

    t.Run("edge: a key stated nowhere is refused outside development", func(t *testing.T) {
        path := writeSettingsFile(t, `{"APP_ENV": "production", "COOKIE_SECURE": true}`)

        if _, err := config.LoadFromFile(path); err == nil {
            t.Fatal("production started with no signing key at all")
        }
    })
}

func TestTheCommittedTemplateIsUsable(t *testing.T) {
    // The template is what every fresh clone actually runs with, and it is the
    // one file that can drift away from this package without anything noticing.
    // A setting renamed here and forgotten there is a stack that will not start.
    const templatePath = "../../config.json.template"

    settings, err := config.LoadFromFile(templatePath)
    if err != nil {
        t.Fatalf("the committed template does not load: %v", err)
    }

    if !settings.IsDevelopment() {
        t.Fatalf("the template is not a development configuration, it says %q", settings.AppEnv)
    }

    if settings.Api.Port != 9000 || settings.Worker.MetricsPort != 9002 {
        t.Fatalf("the template does not carry the documented ports, got api %d and worker %d",
            settings.Api.Port, settings.Worker.MetricsPort)
    }

    if len(settings.AllowedOrigins) != 2 {
        t.Fatalf("the template should list both spellings of the loopback address, got %v",
            settings.AllowedOrigins)
    }

    if settings.Faults.Enabled {
        t.Fatal("the template arms fault injection, which no default may do")
    }

    if settings.Worker.Count != 0 {
        t.Fatalf("the template pins the worker count instead of leaving it at one per processor, got %d",
            settings.Worker.Count)
    }
}

func TestFileIsPresent(t *testing.T) {
    t.Run("a file that is there is reported", func(t *testing.T) {
        if !config.FileIsPresent(writeSettingsFile(t, `{}`)) {
            t.Fatal("an existing settings file was reported missing")
        }
    })

    t.Run("edge: a file that is not there is reported missing", func(t *testing.T) {
        if config.FileIsPresent(filepath.Join(t.TempDir(), "nothing-here.json")) {
            t.Fatal("a missing settings file was reported present")
        }
    })
}
