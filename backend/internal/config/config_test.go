package config_test

import (
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/config"
)

// productionSecret is long enough to pass the length rule, so a test about one
// setting is never failed by another.
const productionSecret = "a-signing-key-long-enough-to-be-accepted-outside-development"

func lookupFrom(values map[string]string) config.LookupFunc {
    return func(key string) (string, bool) {
        value, found := values[key]

        return value, found
    }
}

func mustLoad(t *testing.T, values map[string]string) config.Config {
    t.Helper()

    loaded, err := config.Load(lookupFrom(values))
    if err != nil {
        t.Fatalf("expected the configuration to load, got: %v", err)
    }

    return loaded
}

func loadError(t *testing.T, values map[string]string) string {
    t.Helper()

    _, err := config.Load(lookupFrom(values))
    if err == nil {
        t.Fatal("expected the configuration to be refused, it loaded instead")
    }

    return err.Error()
}

func TestDefaultsAreUsableWithAnEmptyEnvironment(t *testing.T) {
    loaded := mustLoad(t, map[string]string{})

    if loaded.AppEnv != config.EnvironmentDevelopment {
        t.Fatalf("expected development, got %q", loaded.AppEnv)
    }

    if !loaded.IsDevelopment() {
        t.Fatal("the default environment must report itself as development")
    }

    if loaded.Api.Port != 9000 {
        t.Fatalf("expected the api on 9000, got %d", loaded.Api.Port)
    }

    if loaded.Worker.MetricsPort != 9002 {
        t.Fatalf("expected worker metrics on 9002, got %d", loaded.Worker.MetricsPort)
    }

    if loaded.Faults.Enabled {
        t.Fatal("fault injection must be off unless it is switched on deliberately")
    }

    if loaded.Auth.CookieSecure {
        t.Fatal("development runs over plain http, so the secure flag defaults off there")
    }

    if loaded.Auth.JWTSecret.IsEmpty() {
        t.Fatal("development needs a throwaway signing key so a clone runs without setup")
    }
}

func TestEveryValueCanBeOverridden(t *testing.T) {
    loaded := mustLoad(t, map[string]string{
        "APP_ENV":                  config.EnvironmentDevelopment,
        "API_PORT":                 "9100",
        "WORKER_METRICS_PORT":      "9102",
        "DATABASE_MAX_CONNECTIONS": "25",
        "DATABASE_CONNECT_TIMEOUT": "3s",
        "REDIS_ADDRESS":            "redis:6379",
        "ACCESS_TOKEN_TTL":         "5m",
        "REFRESH_TOKEN_TTL":        "48h",
        "FRONTEND_ORIGIN":          "http://localhost:5173",
        "BUILD_VERSION":            "1.2.3",
        "BUILD_COMMIT":             "abc1234",
    })

    if loaded.Api.Port != 9100 || loaded.Worker.MetricsPort != 9102 {
        t.Fatalf("ports were not taken from the environment: %d and %d",
            loaded.Api.Port, loaded.Worker.MetricsPort)
    }

    if loaded.Database.MaxConnections != 25 {
        t.Fatalf("expected 25 connections, got %d", loaded.Database.MaxConnections)
    }

    if loaded.Database.ConnectTimeout != 3*time.Second {
        t.Fatalf("expected a 3s connect timeout, got %s", loaded.Database.ConnectTimeout)
    }

    if loaded.Auth.AccessTokenTTL != 5*time.Minute || loaded.Auth.RefreshTokenTTL != 48*time.Hour {
        t.Fatal("token lifetimes were not taken from the environment")
    }

    if loaded.Build.Version != "1.2.3" || loaded.Build.Commit != "abc1234" {
        t.Fatal("build identity was not taken from the environment")
    }
}

func TestProductionRefusesAMissingOrWeakSigningKey(t *testing.T) {
    t.Run("unit: production without a signing key is refused", func(t *testing.T) {
        message := loadError(t, map[string]string{
            "APP_ENV": config.EnvironmentProduction,
        })

        if !strings.Contains(message, "JWT_SECRET must be set") {
            t.Fatalf("expected the missing key to be named, got: %s", message)
        }
    })

    t.Run("edge: a key one character short is refused", func(t *testing.T) {
        message := loadError(t, map[string]string{
            "APP_ENV":    config.EnvironmentProduction,
            "JWT_SECRET": strings.Repeat("k", 31),
        })

        if !strings.Contains(message, "JWT_SECRET is shorter") {
            t.Fatalf("expected the length rule to fire, got: %s", message)
        }
    })

    t.Run("edge: a key at exactly the minimum is accepted", func(t *testing.T) {
        loaded := mustLoad(t, map[string]string{
            "APP_ENV":    config.EnvironmentProduction,
            "JWT_SECRET": strings.Repeat("k", 32),
        })

        if loaded.IsDevelopment() {
            t.Fatal("production must not report itself as development")
        }

        if !loaded.Auth.CookieSecure {
            t.Fatal("outside development the cookie secure flag defaults on")
        }
    })
}

func TestFaultInjectionCannotBeArmedOutsideDevelopment(t *testing.T) {
    t.Run("unit: development may arm it", func(t *testing.T) {
        loaded := mustLoad(t, map[string]string{
            "APP_ENV":                 config.EnvironmentDevelopment,
            "FAULT_INJECTION_ENABLED": "true",
        })

        if !loaded.Faults.Enabled {
            t.Fatal("development is the one place the surface may exist")
        }
    })

    for _, environment := range []string{config.EnvironmentStaging, config.EnvironmentProduction} {
        t.Run("edge: "+environment+" refuses to start with it on", func(t *testing.T) {
            message := loadError(t, map[string]string{
                "APP_ENV":                 environment,
                "JWT_SECRET":              productionSecret,
                "FAULT_INJECTION_ENABLED": "true",
            })

            if !strings.Contains(message, "FAULT_INJECTION_ENABLED is true") {
                t.Fatalf("expected the fault guard to fire, got: %s", message)
            }
        })
    }
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
    // A service that dies on the first bad value costs one restart per mistake,
    // so the loader has to name all of them together.
    message := loadError(t, map[string]string{
        "APP_ENV":              "prod",
        "API_PORT":             "0",
        "WORKER_METRICS_PORT":  "70000",
        "DATABASE_PRIMARY_URL": "mysql://host:3306/ottodot",
        "REDIS_ADDRESS":        " ",
    })

    expected := []string{
        "APP_ENV is \"prod\"",
        "API_PORT is 0",
        "WORKER_METRICS_PORT is 70000",
        "DATABASE_PRIMARY_URL must use the postgres",
    }

    for _, fragment := range expected {
        if !strings.Contains(message, fragment) {
            t.Fatalf("expected %q in the report, got: %s", fragment, message)
        }
    }
}

func TestMalformedValuesAreNamedByTheirKey(t *testing.T) {
    cases := []struct {
        name     string
        values   map[string]string
        fragment string
    }{
        {
            name:     "edge: a port that is not a number",
            values:   map[string]string{"API_PORT": "nine thousand"},
            fragment: "API_PORT is \"nine thousand\"",
        },
        {
            name:     "edge: a duration without a unit",
            values:   map[string]string{"ACCESS_TOKEN_TTL": "15"},
            fragment: "ACCESS_TOKEN_TTL is \"15\"",
        },
        {
            name:     "edge: a boolean that is neither",
            values:   map[string]string{"FAULT_INJECTION_ENABLED": "maybe"},
            fragment: "FAULT_INJECTION_ENABLED is \"maybe\"",
        },
        {
            name:     "edge: an origin with no scheme",
            values:   map[string]string{"FRONTEND_ORIGIN": "127.0.0.1:9001"},
            fragment: "FRONTEND_ORIGIN is \"127.0.0.1:9001\"",
        },
        {
            name:     "edge: a connection url with no host",
            values:   map[string]string{"DATABASE_REPLICA_URL": "postgres:///ottodot"},
            fragment: "DATABASE_REPLICA_URL has no host",
        },
        {
            name:     "edge: zero connections in the pool",
            values:   map[string]string{"DATABASE_MAX_CONNECTIONS": "0"},
            fragment: "DATABASE_MAX_CONNECTIONS is 0",
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            message := loadError(t, testCase.values)

            if !strings.Contains(message, testCase.fragment) {
                t.Fatalf("expected %q in the report, got: %s", testCase.fragment, message)
            }
        })
    }
}

func TestSettingsThatContradictEachOther(t *testing.T) {
    t.Run("edge: the api and the worker cannot share a port", func(t *testing.T) {
        message := loadError(t, map[string]string{
            "API_PORT":            "9000",
            "WORKER_METRICS_PORT": "9000",
        })

        if !strings.Contains(message, "they must differ") {
            t.Fatalf("expected the port clash to be named, got: %s", message)
        }
    })

    t.Run("edge: a refresh lifetime equal to the access lifetime", func(t *testing.T) {
        message := loadError(t, map[string]string{
            "ACCESS_TOKEN_TTL":  "15m",
            "REFRESH_TOKEN_TTL": "15m",
        })

        if !strings.Contains(message, "rotation can never happen") {
            t.Fatalf("expected the lifetime rule to fire, got: %s", message)
        }
    })

    t.Run("edge: production with the secure cookie flag switched off", func(t *testing.T) {
        message := loadError(t, map[string]string{
            "APP_ENV":       config.EnvironmentProduction,
            "JWT_SECRET":    productionSecret,
            "COOKIE_SECURE": "false",
        })

        if !strings.Contains(message, "travel in clear text") {
            t.Fatalf("expected the cookie rule to fire, got: %s", message)
        }
    })
}

func TestABadConnectionUrlNeverEchoesItsPassword(t *testing.T) {
    const password = "correct-horse-battery-staple"

    message := loadError(t, map[string]string{
        "DATABASE_PRIMARY_URL": "mysql://ottodot:" + password + "@127.0.0.1:5432/ottodot",
    })

    if strings.Contains(message, password) {
        t.Fatalf("the password reached the error text: %s", message)
    }
}
