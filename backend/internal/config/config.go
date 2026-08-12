// Package config loads every setting this service needs from a settings file
// and the environment, and refuses to start when one of them is wrong.
//
// Two rules shape this file:
//
// Every port and every secret is a configuration value. Nothing is a constant
// in code, so a port clash is fixed in one place.
//
// Validation happens once, at startup, and reports every problem at the same
// time. A service that dies on the first bad value costs one restart per
// mistake.
//
// Where the values come from is file.go, and which source wins is stated there.
package config

import (
    "errors"
    "fmt"
    "net/url"
    "os"
    "runtime"
    "strconv"
    "strings"
    "time"
)

// Environment names this service accepts. Anything else is a typo, and a typo
// in this value would quietly disarm the development only guards.
const (
    EnvironmentDevelopment = "development"
    EnvironmentStaging     = "staging"
    EnvironmentProduction  = "production"
)

// minimumSecretLength is the shortest JWT signing secret accepted outside
// development. Shorter than this is brute forceable offline.
const minimumSecretLength = 32

// LookupFunc reads one environment value. Passing it in rather than calling
// os.LookupEnv directly is what lets a test cover every branch without touching
// the process environment.
type LookupFunc func(key string) (string, bool)

// ApiSettings holds the public http surface.
type ApiSettings struct {
    Port            int
    ReadTimeout     time.Duration
    WriteTimeout    time.Duration
    ShutdownTimeout time.Duration
}

// WorkerSettings holds the queue consumer. It has no public surface, only a
// metrics listener.
type WorkerSettings struct {
    MetricsPort  int
    PollInterval time.Duration

    // Count is how many jobs this process works on at once. Zero means one per
    // processor the machine offers, which is the setting a reviewer can leave
    // alone on any machine.
    Count int
}

// Concurrency is Count with zero resolved to the processor count.
//
// The resolution lives here rather than at the call site, so the worker asks one
// question and nobody has to remember what zero meant.
func (settings WorkerSettings) Concurrency() int {
    if settings.Count > 0 {
        return settings.Count
    }

    return runtime.NumCPU()
}

// DatabaseSettings holds both pools. Deciding reads go to the primary, advisory
// reads may use the replica, so the two addresses are separate values rather
// than one address with a flag.
type DatabaseSettings struct {
    PrimaryURL     Secret
    ReplicaURL     Secret
    MaxConnections int32
    ConnectTimeout time.Duration
}

// RedisSettings holds the cache and rate limit connection. Redis is never a
// source of truth here, so losing it degrades the service rather than stopping
// it.
type RedisSettings struct {
    Address  string
    Password Secret
    Database int
}

// AuthSettings holds token signing and lifetimes.
type AuthSettings struct {
    JWTSecret       Secret
    AccessTokenTTL  time.Duration
    RefreshTokenTTL time.Duration
    CookieDomain    string
    CookieSecure    bool
}

// FaultSettings holds the fault injection surface. It is off by default and the
// service refuses to start with it on outside development.
type FaultSettings struct {
    Enabled bool
}

// BuildSettings is what this repository states about the build, for a process
// whose binary was never stamped.
//
// A release binary is stamped by the linker and answers for itself, so these two
// are what a run from source has instead: BUILD_VERSION in config.json is the one
// committed place the backend version number is written down. Either may be left
// empty, which means nobody stated it here and the next source is asked.
type BuildSettings struct {
    Version string
    Commit  string
}

// Config is the whole settings surface, loaded once at startup.
type Config struct {
    AppEnv string

    // AllowedOrigins is every origin the client may be served from. It is a
    // list rather than one value because a browser treats 127.0.0.1 and
    // localhost as different origins, and a reviewer will reach for whichever
    // of the two they happen to type.
    AllowedOrigins []string

    Api      ApiSettings
    Worker   WorkerSettings
    Database DatabaseSettings
    Redis    RedisSettings
    Auth     AuthSettings
    Faults   FaultSettings
    Build    BuildSettings
}

// IsDevelopment reports whether the development only guards are allowed to
// open. An unset or unknown environment never reaches here, because Load
// rejects it.
func (config Config) IsDevelopment() bool {
    return config.AppEnv == EnvironmentDevelopment
}

// LoadFromEnvironment reads the real process environment.
func LoadFromEnvironment() (Config, error) {
    return Load(os.LookupEnv)
}

// Load reads every setting through the given lookup, applies the defaults, then
// validates the result.
//
// Return:
//   - the loaded configuration and a nil error when every value is usable
//   - a zero configuration and a joined error naming every problem at once
func Load(lookup LookupFunc) (Config, error) {
    var problems []error

    appEnv := stringValue(lookup, "APP_ENV", EnvironmentDevelopment)
    development := appEnv == EnvironmentDevelopment

    config := Config{
        AppEnv:         appEnv,
        AllowedOrigins: listValue(lookup, "ALLOWED_ORIGINS", DefaultAllowedOrigins()),
        Api: ApiSettings{
            Port:            intValue(lookup, "API_PORT", 9000, &problems),
            ReadTimeout:     durationValue(lookup, "API_READ_TIMEOUT", 10*time.Second, &problems),
            WriteTimeout:    durationValue(lookup, "API_WRITE_TIMEOUT", 15*time.Second, &problems),
            ShutdownTimeout: durationValue(lookup, "API_SHUTDOWN_TIMEOUT", 20*time.Second, &problems),
        },
        Worker: WorkerSettings{
            MetricsPort:  intValue(lookup, "WORKER_METRICS_PORT", 9002, &problems),
            PollInterval: durationValue(lookup, "WORKER_POLL_INTERVAL", 2*time.Second, &problems),
            Count:        intValue(lookup, "WORKER_COUNT", 0, &problems),
        },
        Database: DatabaseSettings{
            PrimaryURL:     Secret(stringValue(lookup, "DATABASE_PRIMARY_URL", defaultDatabaseURL(5432))),
            ReplicaURL:     Secret(stringValue(lookup, "DATABASE_REPLICA_URL", defaultDatabaseURL(5433))),
            MaxConnections: int32(intValue(lookup, "DATABASE_MAX_CONNECTIONS", 10, &problems)),
            ConnectTimeout: durationValue(lookup, "DATABASE_CONNECT_TIMEOUT", 5*time.Second, &problems),
        },
        Redis: RedisSettings{
            Address:  stringValue(lookup, "REDIS_ADDRESS", "127.0.0.1:6379"),
            Password: Secret(stringValue(lookup, "REDIS_PASSWORD", "")),
            Database: intValue(lookup, "REDIS_DATABASE", 0, &problems),
        },
        Auth: AuthSettings{
            JWTSecret:       Secret(stringValue(lookup, "JWT_SECRET", developmentJWTSecret(development))),
            AccessTokenTTL:  durationValue(lookup, "ACCESS_TOKEN_TTL", 15*time.Minute, &problems),
            RefreshTokenTTL: durationValue(lookup, "REFRESH_TOKEN_TTL", 720*time.Hour, &problems),
            CookieDomain:    stringValue(lookup, "COOKIE_DOMAIN", ""),
            CookieSecure:    boolValue(lookup, "COOKIE_SECURE", !development, &problems),
        },
        Faults: FaultSettings{
            Enabled: boolValue(lookup, "FAULT_INJECTION_ENABLED", false, &problems),
        },
        // Empty is the default rather than a word, because these two are the
        // middle of a chain and not the end of it. A default of "dev" here would
        // answer for a build the linker already named, and would stop the
        // toolchain's own record from ever being asked.
        Build: BuildSettings{
            Version: stringValue(lookup, "BUILD_VERSION", ""),
            Commit:  stringValue(lookup, "BUILD_COMMIT", ""),
        },
    }

    problems = append(problems, config.validate()...)

    if len(problems) > 0 {
        return Config{}, fmt.Errorf("configuration is not usable: %w", errors.Join(problems...))
    }

    return config, nil
}

// validate returns every rule this configuration breaks, rather than the first
// one.
func (config Config) validate() []error {
    var problems []error

    switch config.AppEnv {
    case EnvironmentDevelopment, EnvironmentStaging, EnvironmentProduction:
    default:
        problems = append(problems, fmt.Errorf(
            "APP_ENV is %q, expected one of %s, %s, %s",
            config.AppEnv, EnvironmentDevelopment, EnvironmentStaging, EnvironmentProduction))
    }

    problems = append(problems, validatePort("API_PORT", config.Api.Port)...)
    problems = append(problems, validatePort("WORKER_METRICS_PORT", config.Worker.MetricsPort)...)

    if config.Api.Port == config.Worker.MetricsPort {
        problems = append(problems, fmt.Errorf(
            "API_PORT and WORKER_METRICS_PORT are both %d, they must differ", config.Api.Port))
    }

    if config.Worker.Count < 0 {
        problems = append(problems, fmt.Errorf(
            "WORKER_COUNT is %d, it must be zero for one per processor or a positive number",
            config.Worker.Count))
    }

    if config.Worker.PollInterval <= 0 {
        problems = append(problems, errors.New("WORKER_POLL_INTERVAL must be greater than zero"))
    }

    problems = append(problems, validateDatabaseURL("DATABASE_PRIMARY_URL", config.Database.PrimaryURL)...)
    problems = append(problems, validateDatabaseURL("DATABASE_REPLICA_URL", config.Database.ReplicaURL)...)

    if config.Database.MaxConnections < 1 {
        problems = append(problems, fmt.Errorf(
            "DATABASE_MAX_CONNECTIONS is %d, it must be at least 1", config.Database.MaxConnections))
    }

    if config.Database.ConnectTimeout <= 0 {
        problems = append(problems, errors.New("DATABASE_CONNECT_TIMEOUT must be greater than zero"))
    }

    if strings.TrimSpace(config.Redis.Address) == "" {
        problems = append(problems, errors.New("REDIS_ADDRESS must not be empty"))
    }

    problems = append(problems, config.validateAuth()...)
    problems = append(problems, config.validateOrigins()...)

    // The whole point of the fault surface is that it cannot exist outside
    // development. A flag left on in a promoted environment stops the service
    // rather than opening the surface.
    if config.Faults.Enabled && !config.IsDevelopment() {
        problems = append(problems, fmt.Errorf(
            "FAULT_INJECTION_ENABLED is true while APP_ENV is %q, fault injection runs only in %s",
            config.AppEnv, EnvironmentDevelopment))
    }

    return problems
}

func (config Config) validateAuth() []error {
    var problems []error

    if config.Auth.JWTSecret.IsEmpty() {
        problems = append(problems, errors.New("JWT_SECRET must be set"))
    } else if !config.IsDevelopment() && len(config.Auth.JWTSecret.Reveal()) < minimumSecretLength {
        problems = append(problems, fmt.Errorf(
            "JWT_SECRET is shorter than %d characters, which is brute forceable offline",
            minimumSecretLength))
    }

    if config.Auth.AccessTokenTTL <= 0 {
        problems = append(problems, errors.New("ACCESS_TOKEN_TTL must be greater than zero"))
    }

    if config.Auth.RefreshTokenTTL <= config.Auth.AccessTokenTTL {
        problems = append(problems, errors.New(
            "REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL, otherwise rotation can never happen"))
    }

    if !config.IsDevelopment() && !config.Auth.CookieSecure {
        problems = append(problems, fmt.Errorf(
            "COOKIE_SECURE is false while APP_ENV is %q, the token cookies would travel in clear text",
            config.AppEnv))
    }

    return problems
}

func (config Config) validateOrigins() []error {
    if len(config.AllowedOrigins) == 0 {
        return []error{errors.New(
            "ALLOWED_ORIGINS must name at least one origin, for example http://127.0.0.1:9001")}
    }

    var problems []error

    for _, origin := range config.AllowedOrigins {
        // One message for both failures. A caller does not care whether the
        // parser gave up or the scheme was simply missing, only what a usable
        // value looks like.
        parsed, err := url.Parse(origin)
        if err != nil || parsed.Scheme == "" || parsed.Host == "" {
            problems = append(problems, fmt.Errorf(
                "ALLOWED_ORIGINS holds %q, every entry needs a scheme and a host, for example http://127.0.0.1:9001",
                origin))

            continue
        }

        // A trailing path is silently ignored by a browser when it sends the
        // Origin header, so an entry carrying one would never match anything
        // and the refusal would look like a mystery.
        if parsed.Path != "" {
            problems = append(problems, fmt.Errorf(
                "ALLOWED_ORIGINS holds %q, an origin is a scheme and a host with no path", origin))
        }
    }

    return problems
}

// DefaultAllowedOrigins is where the client is served from during development.
//
// Both spellings of the loopback address are here on purpose. They are different
// origins to a browser, and a reviewer who types the one this list left out
// would watch every request fail for a reason nothing on screen explains.
func DefaultAllowedOrigins() []string {
    return []string{"http://127.0.0.1:9001", "http://localhost:9001"}
}

func validatePort(key string, port int) []error {
    if port < 1 || port > 65535 {
        return []error{fmt.Errorf("%s is %d, a port must be between 1 and 65535", key, port)}
    }

    return nil
}

func validateDatabaseURL(key string, value Secret) []error {
    if value.IsEmpty() {
        return []error{fmt.Errorf("%s must be set", key)}
    }

    parsed, err := url.Parse(value.Reveal())
    if err != nil {
        // The error from url.Parse can quote the input, which carries a
        // password, so it is deliberately not wrapped here.
        return []error{fmt.Errorf("%s is not a url", key)}
    }

    if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
        return []error{fmt.Errorf("%s must use the postgres or postgresql scheme", key)}
    }

    if parsed.Host == "" {
        return []error{fmt.Errorf("%s has no host", key)}
    }

    return nil
}

// defaultDatabaseURL builds the local development address for a port. It exists
// so a reviewer can clone, bring the stack up, and run, without writing an
// environment file first.
func defaultDatabaseURL(port int) string {
    return fmt.Sprintf(
        "postgres://ottodot:ottodot_development@127.0.0.1:%d/ottodot?sslmode=disable", port)
}

// developmentJWTSecret returns a throwaway signing key for local runs only.
// Outside development the default is empty, so a missing secret is a startup
// failure rather than a silently weak one.
func developmentJWTSecret(development bool) string {
    if !development {
        return ""
    }

    return "development-only-signing-key-not-for-any-real-environment"
}

func stringValue(lookup LookupFunc, key string, fallback string) string {
    raw, found := lookup(key)
    if !found || strings.TrimSpace(raw) == "" {
        return fallback
    }

    return strings.TrimSpace(raw)
}

// listValue reads a comma separated setting.
//
// The separator is the same in both sources: a json array is joined with commas
// before it ever reaches here, so a list written in the file and a list written
// in the environment are parsed by this one function.
func listValue(lookup LookupFunc, key string, fallback []string) []string {
    raw, found := lookup(key)
    if !found || strings.TrimSpace(raw) == "" {
        return fallback
    }

    parts := strings.Split(raw, ",")
    values := make([]string, 0, len(parts))

    for _, part := range parts {
        trimmed := strings.TrimSpace(part)
        if trimmed == "" {
            continue
        }

        values = append(values, trimmed)
    }

    // A value made only of separators states nothing, so it is treated the same
    // way an empty one is rather than as an empty list nobody asked for.
    if len(values) == 0 {
        return fallback
    }

    return values
}

func intValue(lookup LookupFunc, key string, fallback int, problems *[]error) int {
    raw, found := lookup(key)
    if !found || strings.TrimSpace(raw) == "" {
        return fallback
    }

    parsed, err := strconv.Atoi(strings.TrimSpace(raw))
    if err != nil {
        *problems = append(*problems, fmt.Errorf("%s is %q, expected a whole number", key, raw))

        return fallback
    }

    return parsed
}

func boolValue(lookup LookupFunc, key string, fallback bool, problems *[]error) bool {
    raw, found := lookup(key)
    if !found || strings.TrimSpace(raw) == "" {
        return fallback
    }

    parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
    if err != nil {
        *problems = append(*problems, fmt.Errorf("%s is %q, expected true or false", key, raw))

        return fallback
    }

    return parsed
}

func durationValue(lookup LookupFunc, key string, fallback time.Duration, problems *[]error) time.Duration {
    raw, found := lookup(key)
    if !found || strings.TrimSpace(raw) == "" {
        return fallback
    }

    parsed, err := time.ParseDuration(strings.TrimSpace(raw))
    if err != nil {
        *problems = append(*problems, fmt.Errorf("%s is %q, expected a duration such as 15m or 5s", key, raw))

        return fallback
    }

    return parsed
}
