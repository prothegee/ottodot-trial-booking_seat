package httpx_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/catalogue"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/operations"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/ratelimit"
    "ottodot-trial-booking/backend/internal/roster"
)

// The identifiers every case in this package uses. They are fixed rather than
// minted, so a failure names the same child every time it is read.
const (
    classOpen = "11111111-1111-7111-8111-111111111111"
    classOne  = "22222222-2222-7222-8222-222222222222"

    studentOne = "aaaaaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa"
    studentTwo = "bbbbbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb"

    parentOne   = "cccccccc-cccc-7ccc-8ccc-cccccccccccc"
    parentTwo   = "dddddddd-dddd-7ddd-8ddd-dddddddddddd"
    adminParent = "eeeeeeee-eeee-7eee-8eee-eeeeeeeeeeee"

    // studentOther belongs to the second parent, which is what makes the
    // ownership check provable rather than merely present.
    studentOther = "ffffffff-ffff-7fff-8fff-ffffffffffff"
)

// frontendOrigin is the one origin this service serves in a test.
const frontendOrigin = "http://127.0.0.1:9001"

// jwtSecret is long enough for the signer to accept and is a throwaway.
const jwtSecret = "test-only-signing-key-not-for-any-real-environment"

// stageMoment is the instant every case starts from.
var stageMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// stage is the whole api wired against fakes, with each fake reachable so a case
// can assert on what it holds.
type stage struct {
    router http.Handler

    bookings  *booking.MemoryRepository
    payments  *payment.MemoryRepository
    provider  *payment.MockProvider
    catalogue *catalogue.MemoryReader
    jobs      *queue.MemoryQueue
    cache     *cache.MemoryStore
    limiter   *ratelimit.MemoryLimiter
    verifier  *captcha.MockVerifier
    directory *auth.MemoryDirectory
    signer    *auth.Signer

    // now is the clock every layer reads. A case moves it to age a bucket or a
    // token without sleeping.
    now time.Time
}

// stageOptions are the few things a case wants to vary.
type stageOptions struct {
    // WriteRule overrides the write bucket, so a flood case can empty it in
    // three requests instead of thirteen.
    WriteRule ratelimit.Bucket

    // ReadRule overrides the read bucket.
    ReadRule ratelimit.Bucket

    // RequireCaptcha refuses a submission with no challenge token.
    RequireCaptcha bool

    // Development accepts the two amounts the mock provider reads as a decline
    // and as an unreachable provider.
    Development bool
}

// newStage wires every layer against a fake and returns the router.
func newStage(t *testing.T, options stageOptions) *stage {
    t.Helper()

    fixture := &stage{now: stageMoment}

    fixture.bookings = booking.NewMemoryRepository()
    fixture.bookings.AddClass(booking.Class{
        ID: classOpen, Subject: "science", Title: "Science trial",
        StartsAt: stageMoment.Add(24 * time.Hour), DurationMinutes: 60, Capacity: 4, HoldAllowance: 2,
    })
    fixture.bookings.AddClass(booking.Class{
        ID: classOne, Subject: "math", Title: "Math trial",
        StartsAt: stageMoment.Add(48 * time.Hour), DurationMinutes: 60, Capacity: 1, HoldAllowance: 2,
    })
    fixture.bookings.AddStudent(studentOne, parentOne)
    fixture.bookings.AddStudent(studentTwo, parentOne)
    fixture.bookings.AddStudent(studentOther, parentTwo)

    fixture.catalogue = catalogue.NewMemoryReader()
    fixture.catalogue.AddClass(catalogue.Class{
        ID: classOpen, Subject: "science", Title: "Science trial",
        StartsAt: stageMoment.Add(24 * time.Hour), DurationMinutes: 60, Capacity: 4, SeatsRemaining: 4,
    })
    fixture.catalogue.AddClass(catalogue.Class{
        ID: classOne, Subject: "math", Title: "Math trial",
        StartsAt: stageMoment.Add(48 * time.Hour), DurationMinutes: 60, Capacity: 1, SeatsRemaining: 1,
    })

    rosterReader := roster.NewMemoryReader()
    rosterReader.AddClass(classOpen, 4)
    rosterReader.AddClass(classOne, 1)

    fixture.directory = auth.NewMemoryDirectory()
    fixture.directory.Add("alice.tan@example.test", auth.Parent{
        ID: parentOne, DisplayName: "Alice Tan", Role: auth.RoleParent,
    }, []auth.Child{
        {ID: studentOne, FullName: "Adi Tan", GradeLevel: 4},
        {ID: studentTwo, FullName: "Bella Tan", GradeLevel: 6},
    })
    fixture.directory.Add("budi.santoso@example.test", auth.Parent{
        ID: parentTwo, DisplayName: "Budi Santoso", Role: auth.RoleParent,
    }, []auth.Child{
        {ID: studentOther, FullName: "Citra Santoso", GradeLevel: 5},
    })
    fixture.directory.Add("ops.admin@example.test", auth.Parent{
        ID: adminParent, DisplayName: "Operations", Role: auth.RoleAdmin,
    }, nil)

    clock := func() time.Time { return fixture.now }

    bookingSettings := booking.DefaultSettings()
    bookingSettings.Clock = clock

    bookingService, err := booking.NewService(fixture.bookings, bookingSettings)
    if err != nil {
        t.Fatalf("cannot build the booking service: %v", err)
    }

    fixture.payments = payment.NewMemoryRepository()
    fixture.provider = payment.NewMockProvider()

    paymentService, err := payment.NewService(fixture.payments, fixture.provider, payment.Settings{Clock: clock})
    if err != nil {
        t.Fatalf("cannot build the payment service: %v", err)
    }

    fixture.jobs = queue.NewMemoryQueue()

    checkoutSettings := checkout.DefaultSettings()
    checkoutSettings.Clock = clock

    checkoutService, err := checkout.NewService(bookingService, paymentService, fixture.jobs, checkoutSettings)
    if err != nil {
        t.Fatalf("cannot build the checkout service: %v", err)
    }

    classService, err := catalogue.NewService(fixture.catalogue)
    if err != nil {
        t.Fatalf("cannot build the catalogue service: %v", err)
    }

    rosterService, err := roster.NewService(rosterReader)
    if err != nil {
        t.Fatalf("cannot build the roster service: %v", err)
    }

    fixture.signer, err = auth.NewSigner(jwtSecret)
    if err != nil {
        t.Fatalf("cannot build the signer: %v", err)
    }

    denylist := auth.NewMemoryDenylist()

    authSettings := auth.DefaultSettings()
    authSettings.Clock = clock

    authService, err := auth.NewService(fixture.signer, auth.NewMemoryRefreshStore(), fixture.directory, denylist, authSettings)
    if err != nil {
        t.Fatalf("cannot build the auth service: %v", err)
    }

    guard, err := auth.NewGuard(fixture.signer, denylist, auth.GuardSettings{
        FrontendOrigin: frontendOrigin,
        Clock:          clock,
    })
    if err != nil {
        t.Fatalf("cannot build the guard: %v", err)
    }

    authHandler, err := auth.NewHandler(authService, auth.NewCookieWriter(auth.CookieSettings{}), guard)
    if err != nil {
        t.Fatalf("cannot build the auth handler: %v", err)
    }

    counters := httpx.NewCounters()

    fixture.cache = cache.NewMemoryStore()
    fixture.cache.SetClock(clock)

    conditional, err := httpx.NewConditional(fixture.cache, cache.DefaultLifetime, counters)
    if err != nil {
        t.Fatalf("cannot build the conditional reads: %v", err)
    }

    fixture.limiter = ratelimit.NewMemoryLimiter()

    limits, err := httpx.NewLimits(fixture.limiter, clock, counters)
    if err != nil {
        t.Fatalf("cannot build the rate limits: %v", err)
    }

    owner, err := httpx.NewOwner(fixture.directory, bookingService, counters)
    if err != nil {
        t.Fatalf("cannot build the ownership check: %v", err)
    }

    fixture.verifier = captcha.NewMockVerifier()

    botCheck, err := httpx.NewBotCheck(httpx.BotCheckSettings{
        Verifier:       fixture.verifier,
        RequireCaptcha: options.RequireCaptcha,
    }, counters)
    if err != nil {
        t.Fatalf("cannot build the bot check: %v", err)
    }

    readiness, err := operations.NewReadiness([]operations.Dependency{{
        Name:     "postgres_primary",
        Required: true,
        Probe:    func(context.Context) error { return nil },
    }})
    if err != nil {
        t.Fatalf("cannot build readiness: %v", err)
    }

    operationsHandler, err := operations.NewHandler(readiness, operations.NewIdentity("0.1.0", "6b30337", ""))
    if err != nil {
        t.Fatalf("cannot build the operations handler: %v", err)
    }

    classHandler, err := httpx.NewClassHandler(classService, conditional)
    if err != nil {
        t.Fatalf("cannot build the class routes: %v", err)
    }

    studentHandler, err := httpx.NewStudentHandler(fixture.directory)
    if err != nil {
        t.Fatalf("cannot build the student route: %v", err)
    }

    bookingHandler, err := httpx.NewBookingHandler(checkoutService, bookingService, owner, botCheck, conditional)
    if err != nil {
        t.Fatalf("cannot build the booking routes: %v", err)
    }

    paymentHandler, err := httpx.NewPaymentHandler(checkoutService, owner, botCheck, conditional, options.Development)
    if err != nil {
        t.Fatalf("cannot build the payment route: %v", err)
    }

    rosterHandler, err := httpx.NewRosterHandler(rosterService)
    if err != nil {
        t.Fatalf("cannot build the roster route: %v", err)
    }

    adminHandler, err := httpx.NewAdminHandler(bookingService, fixture.jobs, clock)
    if err != nil {
        t.Fatalf("cannot build the admin routes: %v", err)
    }

    router, err := httpx.NewRouter(httpx.Routes{
        Operations: operationsHandler,
        Auth:       authHandler,
        Classes:    classHandler,
        Students:   studentHandler,
        Bookings:   bookingHandler,
        Payments:   paymentHandler,
        Roster:     rosterHandler,
        Admin:      adminHandler,
        Guard:      guard,
        Limits:     limits,
    })
    if err != nil {
        t.Fatalf("cannot build the router: %v", err)
    }

    fixture.router = withRules(router, options)

    return fixture
}

// withRules is a no-op today and exists so the rule overrides in stageOptions
// have one place to be applied if the router ever takes them.
//
// The buckets the router uses are the package defaults, and a case that needs a
// tighter one drives the limiter directly. Keeping that in one function stops
// each case inventing its own way of doing it.
func withRules(router http.Handler, _ stageOptions) http.Handler {
    return router
}

// tokenFor mints an access token for one parent, exactly as sign in would.
func (fixture *stage) tokenFor(t *testing.T, parentID string, role string) string {
    t.Helper()

    token, err := fixture.signer.Sign(auth.Claims{
        Subject:   parentID,
        Role:      role,
        Type:      auth.TypeAccess,
        TokenID:   "99999999-9999-7999-8999-999999999999",
        IssuedAt:  fixture.now.Unix(),
        ExpiresAt: fixture.now.Add(15 * time.Minute).Unix(),
    })
    if err != nil {
        t.Fatalf("cannot sign a token: %v", err)
    }

    return token
}

// request is one call to the api, described the way a case wants to describe it.
type request struct {
    method string
    path   string
    body   string

    // parent is who the request is signed in as. An empty value sends no
    // cookie at all, which is the anonymous case.
    parent string
    role   string

    // origin overrides the Origin header. An empty value sends the one this
    // service serves on a write, and nothing on a read.
    origin string

    // omitOrigin sends no Origin header even on a write, which is what a caller
    // that is not a browser looks like.
    omitOrigin bool

    idempotencyKey string
    ifNoneMatch    string
    remoteAddress  string
}

// send drives one request through the whole chain.
func (fixture *stage) send(t *testing.T, call request) *httptest.ResponseRecorder {
    t.Helper()

    var body *strings.Reader

    if call.body == "" {
        body = strings.NewReader("")
    } else {
        body = strings.NewReader(call.body)
    }

    httpRequest := httptest.NewRequest(call.method, call.path, body)

    if call.remoteAddress != "" {
        httpRequest.RemoteAddr = call.remoteAddress
    }

    if call.parent != "" {
        role := call.role
        if role == "" {
            role = auth.RoleParent
        }

        httpRequest.AddCookie(&http.Cookie{
            Name:  auth.AccessCookieName,
            Value: fixture.tokenFor(t, call.parent, role),
        })
    }

    if !isSafe(call.method) && !call.omitOrigin {
        origin := call.origin
        if origin == "" {
            origin = frontendOrigin
        }

        httpRequest.Header.Set("Origin", origin)
    }

    if call.origin != "" && isSafe(call.method) {
        httpRequest.Header.Set("Origin", call.origin)
    }

    if call.idempotencyKey != "" {
        httpRequest.Header.Set(httpx.IdempotencyKeyHeader, call.idempotencyKey)
    }

    if call.ifNoneMatch != "" {
        httpRequest.Header.Set("If-None-Match", call.ifNoneMatch)
    }

    if call.body != "" {
        httpRequest.Header.Set("Content-Type", "application/json")
    }

    recorder := httptest.NewRecorder()
    fixture.router.ServeHTTP(recorder, httpRequest)

    return recorder
}

// isSafe reports whether a method changes nothing.
func isSafe(method string) bool {
    return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// failureBody is the envelope, decoded.
type failureBody struct {
    Error struct {
        Code              string `json:"code"`
        Message           string `json:"message"`
        RetryAfterSeconds int    `json:"retry_after_seconds"`
        RequestID         string `json:"request_id"`
        BookingID         string `json:"booking_id"`
    } `json:"error"`
}

// failureOf decodes a refusal and fails the case if the body is not one.
func failureOf(t *testing.T, recorder *httptest.ResponseRecorder) failureBody {
    t.Helper()

    var decoded failureBody

    if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
        t.Fatalf("the body is not an envelope: %v, body %s", err, recorder.Body.String())
    }

    if decoded.Error.Code == "" {
        t.Fatalf("the body carries no code: %s", recorder.Body.String())
    }

    return decoded
}

// decodeInto reads a successful body.
func decodeInto(t *testing.T, recorder *httptest.ResponseRecorder, into any) {
    t.Helper()

    if err := json.Unmarshal(recorder.Body.Bytes(), into); err != nil {
        t.Fatalf("the body is not readable: %v, body %s", err, recorder.Body.String())
    }
}

// holdBody is the json a hold request sends, with the bot signals a real client
// carries.
func holdBody(studentID string, classID string) string {
    return `{"student_id":"` + studentID + `","class_id":"` + classID +
        `","website":"","filled_in_ms":4200,"captcha_token":"` + captcha.TokenPass + `"}`
}

// payBody is the json a payment request sends.
func payBody(cents int) string {
    return `{"amount_cents":` + itoa(cents) +
        `,"currency":"SGD","website":"","filled_in_ms":4200,"captcha_token":"` + captcha.TokenPass + `"}`
}

// itoa is here so the two body builders read as one line each.
func itoa(value int) string {
    if value == 0 {
        return "0"
    }

    digits := ""

    for value > 0 {
        digits = string(rune('0'+value%10)) + digits
        value /= 10
    }

    return digits
}

// bookingWire is one booking as the client reads it.
type bookingWire struct {
    ID            string  `json:"id"`
    StudentID     string  `json:"student_id"`
    ClassID       string  `json:"class_id"`
    Status        string  `json:"status"`
    SeatNo        *int16  `json:"seat_no"`
    HoldExpiresAt *string `json:"hold_expires_at"`
}

// holdOne books a seat through the api and returns what came back.
func (fixture *stage) holdOne(t *testing.T, studentID string, classID string) bookingWire {
    t.Helper()

    recorder := fixture.send(t, request{
        method:         http.MethodPost,
        path:           httpx.CreateBookingPath[len("POST "):],
        body:           holdBody(studentID, classID),
        parent:         parentOne,
        idempotencyKey: "11111111-1111-4111-8111-111111111111",
    })

    if recorder.Code != http.StatusCreated {
        t.Fatalf("the hold answered %d: %s", recorder.Code, recorder.Body.String())
    }

    var granted bookingWire

    decodeInto(t, recorder, &granted)

    // The payment fake stands in for the foreign key the real table has, so a
    // booking it has never heard of cannot be charged.
    fixture.payments.AddBooking(granted.ID)

    return granted
}
