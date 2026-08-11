// Command api serves the http surface.
//
// It owns no rule. Every decision this binary makes has already been made in a
// package that can be tested without a socket, and this file is the one place
// that says which implementation of each of those is used in a running service.
// That is why it reads as a list of constructors: the wiring is the only thing
// here, and the wiring is worth reading carefully.
//
// Two choices in it are load bearing and are commented where they are made:
// which pool each read goes to, and what the service does when Redis is not
// there.
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/catalogue"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/operations"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/ratelimit"
    "ottodot-trial-booking/backend/internal/roster"
)

// startupTimeout caps how long the process waits for its dependencies. Past it,
// a wrong address is a startup failure with a clear message rather than a
// process that hangs and looks alive.
const startupTimeout = 30 * time.Second

// buildVersion, buildCommit, and buildTime are stamped at link time by
// containers/Containerfile.api. They are values rather than constants for
// exactly that reason, and the defaults are what a local go build produces.
var (
    buildVersion = "dev"
    buildCommit  = "unknown"
    buildTime    = ""
)

// listenAddress is where the api binds.
//
// It binds every interface rather than the loopback one, and the reason is worth
// being exact about, because the project rule reads the other way. This process
// runs inside a container, and a container that binds 127.0.0.1 is reachable by
// nothing at all, including the port its own compose file publishes.
//
// The loopback restriction is real and it is enforced one layer out, in compose,
// where the port is published as `127.0.0.1:9000:9000`.
func listenAddress(port int) string {
    return fmt.Sprintf(":%d", port)
}

// openRedis connects to the shared store.
//
// It pings eagerly for the same reason the pools do: a wrong address should be a
// startup failure with a readable message, not a confusing one on the first
// parent's booking.
func openRedis(ctx context.Context, settings config.RedisSettings) (redis.UniversalClient, error) {
    client := redis.NewClient(&redis.Options{
        Addr:     settings.Address,
        Password: settings.Password.Reveal(),
        DB:       settings.Database,
    })

    if err := client.Ping(ctx).Err(); err != nil {
        _ = client.Close()

        // The driver error can echo the address it was dialling, and an address
        // carries a password, so nothing from it is wrapped into what is
        // printed.
        return nil, errors.New("redis is not reachable")
    }

    return client, nil
}

// buildAuth wires the session half.
//
// Every pool here is the primary. A refresh token lookup decides whether a token
// has already been spent, and a revoked token that still works because a replica
// is a second behind is a security hole rather than a display quirk.
func buildAuth(pools *database.Pools, client redis.UniversalClient, settings config.Config) (*auth.Service, *auth.Guard, *auth.Handler, error) {
    signer, err := auth.NewSigner(settings.Auth.JWTSecret.Reveal())
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the token signer: %w", err)
    }

    denylist, err := auth.NewRedisDenylist(client)
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the token denylist: %w", err)
    }

    authSettings := auth.DefaultSettings()
    authSettings.AccessTTL = settings.Auth.AccessTokenTTL
    authSettings.RefreshTTL = settings.Auth.RefreshTokenTTL

    service, err := auth.NewService(
        signer,
        auth.NewPostgresRefreshStore(pools.Primary()),
        auth.NewPostgresDirectory(pools.Primary()),
        denylist,
        authSettings)
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the auth service: %w", err)
    }

    guard, err := auth.NewGuard(signer, denylist, auth.GuardSettings{
        FrontendOrigin: settings.FrontendOrigin,
    })
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the request guard: %w", err)
    }

    cookies := auth.NewCookieWriter(auth.CookieSettings{
        Domain: settings.Auth.CookieDomain,
        Secure: settings.Auth.CookieSecure,
    })

    handler, err := auth.NewHandler(service, cookies, guard)
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the auth routes: %w", err)
    }

    return service, guard, handler, nil
}

// buildCheckout wires the seat, the money, and the queue into the order a
// checkout happens in.
//
// Everything here is the primary. A seat is decided, a charge is written, and a
// job has to survive a restart, and none of those may read a replica.
func buildCheckout(pools *database.Pools) (*checkout.Service, *booking.Service, queue.Queue, error) {
    bookings, err := booking.NewService(booking.NewPostgresRepository(pools.Primary()), booking.DefaultSettings())
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the booking service: %w", err)
    }

    payments, err := payment.NewService(
        payment.NewPostgresRepository(pools.Primary()),
        payment.NewMockProvider(),
        payment.Settings{})
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the payment service: %w", err)
    }

    jobs := queue.NewPostgresQueue(pools.Primary())

    service, err := checkout.NewService(bookings, payments, jobs, checkout.DefaultSettings())
    if err != nil {
        return nil, nil, nil, fmt.Errorf("the checkout service: %w", err)
    }

    return service, bookings, jobs, nil
}

// buildRoutes assembles the whole surface.
//
// The two advisory readers are the only things in this function pointed at the
// replica, and that is the read routing decision in one place: a class list and
// a roster are read minutes before they matter and may be a second behind, and
// everything that decides anything is above, on the primary.
func buildRoutes(pools *database.Pools, client redis.UniversalClient, settings config.Config) (http.Handler, error) {
    counters := httpx.NewCounters()

    _, guard, authHandler, err := buildAuth(pools, client, settings)
    if err != nil {
        return nil, err
    }

    checkoutService, bookings, jobs, err := buildCheckout(pools)
    if err != nil {
        return nil, err
    }

    classes, err := catalogue.NewService(catalogue.NewPostgresReader(pools.Replica()))
    if err != nil {
        return nil, fmt.Errorf("the catalogue: %w", err)
    }

    rosters, err := roster.NewService(roster.NewPostgresReader(pools.Replica()))
    if err != nil {
        return nil, fmt.Errorf("the roster: %w", err)
    }

    store, err := cache.NewRedisStore(client)
    if err != nil {
        return nil, fmt.Errorf("the response cache: %w", err)
    }

    conditional, err := httpx.NewConditional(store, cache.DefaultLifetime, counters)
    if err != nil {
        return nil, fmt.Errorf("the conditional reads: %w", err)
    }

    limiter, err := ratelimit.NewRedisLimiter(client)
    if err != nil {
        return nil, fmt.Errorf("the rate limiter: %w", err)
    }

    limits, err := httpx.NewLimits(limiter, nil, counters)
    if err != nil {
        return nil, fmt.Errorf("the rate limit middleware: %w", err)
    }

    directory := auth.NewPostgresDirectory(pools.Primary())

    owner, err := httpx.NewOwner(directory, bookings, counters)
    if err != nil {
        return nil, fmt.Errorf("the ownership check: %w", err)
    }

    botCheck, err := httpx.NewBotCheck(httpx.BotCheckSettings{
        Verifier: captcha.NewMockVerifier(),
    }, counters)
    if err != nil {
        return nil, fmt.Errorf("the bot check: %w", err)
    }

    readiness, err := operations.NewReadiness(readinessChecks(pools, client))
    if err != nil {
        return nil, fmt.Errorf("readiness: %w", err)
    }

    operationsHandler, err := operations.NewHandler(readiness,
        operations.NewIdentity(buildVersion, buildCommit, buildTime))
    if err != nil {
        return nil, fmt.Errorf("the operations routes: %w", err)
    }

    return assembleRoutes(routeParts{
        operations:  operationsHandler,
        auth:        authHandler,
        classes:     classes,
        rosters:     rosters,
        bookings:    bookings,
        checkout:    checkoutService,
        jobs:        jobs,
        directory:   directory,
        owner:       owner,
        botCheck:    botCheck,
        conditional: conditional,
        guard:       guard,
        limits:      limits,
        development: settings.IsDevelopment(),
    })
}

// routeParts is everything assembleRoutes needs, named rather than positional,
// because a dozen arguments in a row is a dozen chances to swap two of them.
type routeParts struct {
    operations  *operations.Handler
    auth        *auth.Handler
    classes     *catalogue.Service
    rosters     *roster.Service
    bookings    *booking.Service
    checkout    *checkout.Service
    jobs        queue.Queue
    directory   auth.Directory
    owner       *httpx.Owner
    botCheck    *httpx.BotCheck
    conditional *httpx.Conditional
    guard       *auth.Guard
    limits      *httpx.Limits
    development bool
}

// assembleRoutes builds each handler and hands them to the router.
func assembleRoutes(parts routeParts) (http.Handler, error) {
    classHandler, err := httpx.NewClassHandler(parts.classes, parts.conditional)
    if err != nil {
        return nil, fmt.Errorf("the class routes: %w", err)
    }

    studentHandler, err := httpx.NewStudentHandler(parts.directory)
    if err != nil {
        return nil, fmt.Errorf("the student route: %w", err)
    }

    bookingHandler, err := httpx.NewBookingHandler(parts.checkout, parts.bookings, parts.owner, parts.botCheck, parts.conditional)
    if err != nil {
        return nil, fmt.Errorf("the booking routes: %w", err)
    }

    paymentHandler, err := httpx.NewPaymentHandler(parts.checkout, parts.owner, parts.botCheck, parts.conditional, parts.development)
    if err != nil {
        return nil, fmt.Errorf("the payment route: %w", err)
    }

    rosterHandler, err := httpx.NewRosterHandler(parts.rosters)
    if err != nil {
        return nil, fmt.Errorf("the roster route: %w", err)
    }

    adminHandler, err := httpx.NewAdminHandler(parts.bookings, parts.jobs, nil)
    if err != nil {
        return nil, fmt.Errorf("the admin routes: %w", err)
    }

    return httpx.NewRouter(httpx.Routes{
        Operations: parts.operations,
        Auth:       parts.auth,
        Classes:    classHandler,
        Students:   studentHandler,
        Bookings:   bookingHandler,
        Payments:   paymentHandler,
        Roster:     rosterHandler,
        Admin:      adminHandler,
        Guard:      parts.guard,
        Limits:     parts.limits,
        Recovery: func(requestID string, err error) {
            log.Printf("api: request %s: %v", requestID, err)
        },
    })
}

// readinessChecks is what /readyz probes, and which failures take this service
// out of rotation.
//
// The replica is advisory. Every deciding read already goes to the primary, so a
// replica that is down costs nothing that matters and reporting unready would
// take a working service out for no reason. The primary and Redis are required:
// without the first no seat can be decided, and without the second the denylist
// cannot say no, which means a signed out token would still be believed.
func readinessChecks(pools *database.Pools, client redis.UniversalClient) []operations.Dependency {
    return []operations.Dependency{
        {
            Name:     "postgres_primary",
            Required: true,
            Probe:    pools.PingPrimary,
        },
        {
            Name:     "postgres_replica",
            Required: false,
            Probe:    pools.PingReplica,
        },
        {
            Name:     "redis",
            Required: true,
            Probe: func(ctx context.Context) error {
                return client.Ping(ctx).Err()
            },
        },
    }
}

// serve starts the listener and reports a failure that is not the ordinary
// shutdown.
func serve(listener *http.Server) {
    if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Printf("api: the listener stopped: %v", err)
    }
}

// run is the whole process, written to return an error rather than to exit, so
// every failure leaves through one place.
func run() error {
    settings, err := config.LoadFromEnvironment()
    if err != nil {
        return fmt.Errorf("the configuration was refused: %w", err)
    }

    startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
    defer cancelStartup()

    pools, err := database.Open(startupCtx, database.Settings{
        PrimaryURL:     settings.Database.PrimaryURL.Reveal(),
        ReplicaURL:     settings.Database.ReplicaURL.Reveal(),
        MaxConnections: settings.Database.MaxConnections,
        ConnectTimeout: settings.Database.ConnectTimeout,
    })
    if err != nil {
        return fmt.Errorf("the database is not reachable: %w", err)
    }

    defer pools.Close()

    client, err := openRedis(startupCtx, settings.Redis)
    if err != nil {
        return fmt.Errorf("redis: %w", err)
    }

    defer client.Close()

    handler, err := buildRoutes(pools, client, settings)
    if err != nil {
        return fmt.Errorf("the api could not be wired: %w", err)
    }

    address := listenAddress(settings.Api.Port)

    listener := &http.Server{
        Addr:         address,
        Handler:      handler,
        ReadTimeout:  settings.Api.ReadTimeout,
        WriteTimeout: settings.Api.WriteTimeout,
        IdleTimeout:  2 * settings.Api.ReadTimeout,
    }

    go serve(listener)

    log.Printf("api: version %s, commit %s, serving on %s", buildVersion, buildCommit, address)

    // The signal context is what ends this. Everything below it unwinds in
    // order: stop accepting, let in flight requests finish, then close the
    // pools and the client through the deferred calls above.
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    <-ctx.Done()

    shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.Api.ShutdownTimeout)
    defer cancelShutdown()

    if err := listener.Shutdown(shutdownCtx); err != nil {
        log.Printf("api: the listener did not close cleanly: %v", err)
    }

    log.Print("api: stopped")

    return nil
}

func main() {
    if err := run(); err != nil {
        log.Printf("api: %v", err)
        os.Exit(1)
    }
}
