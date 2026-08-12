package observability

// The metric names this service publishes.
//
// They are constants in one file because three separate things name the same
// string and none of them can see the others: the code that increments it, the
// Grafana panel that draws it, and the alert rule that fires on it. A rename that
// updates only the first turns a panel blank and an alert silent, and neither
// failure announces itself.
//
// That is also why phase 7 carries a test that reads the dashboard json and the
// rule file and asserts every metric they mention is one of these. A metric name
// is a contract with a file nobody compiles.
const (
    // Access failures.
    MetricAccessDenied       = "access_denied_total"
    MetricRateLimitRejected  = "rate_limit_rejected_total"
    MetricBotCheckRejected   = "bot_check_rejected_total"
    MetricAuthTokenIssued    = "auth_token_issued_total"
    MetricAuthRefreshReuse   = "auth_refresh_reuse_detected_total"
    MetricAuthRefreshRotated = "auth_refresh_rotated_total"
    MetricAuthLoginRefused   = "auth_login_refused_total"

    // Transaction failures.
    MetricDatabaseTransaction    = "db_transaction_total"
    MetricConfirmTransaction     = "confirm_transaction_total"
    MetricConfirmDurationSeconds = "confirm_transaction_duration_seconds"
    MetricPaymentAttempt         = "payment_attempt_total"
    MetricQueueJobFailed         = "queue_job_failed_total"
    MetricRefundPendingBookings  = "refund_pending_bookings"

    // Application.
    MetricRequestDurationSeconds  = "http_request_duration_seconds"
    MetricNotModified             = "http_not_modified_total"
    MetricPanicRecovered          = "panic_recovered_total"
    MetricHoldGranted             = "booking_hold_granted_total"
    MetricBookingConfirmed        = "booking_confirmed_total"
    MetricRaceLost                = "booking_race_lost_total"
    MetricDuplicateRejected       = "booking_duplicate_rejected_total"
    MetricHoldExpired             = "booking_hold_expired_total"
    MetricQueueDepth              = "queue_depth"
    MetricQueueDepthUnread        = "queue_depth_read_failed_total"
    MetricQueueJobDuration        = "queue_job_duration_seconds"
    MetricWorkerJobsClaimed       = "worker_jobs_claimed_total"
    MetricWorkerJobsCompleted     = "worker_jobs_completed_total"
    MetricCacheLookup             = "cache_lookup_total"
    MetricDatabasePoolConnections = "db_pool_connections"
    MetricReplicationLagBytes     = "replication_lag_bytes"
    MetricFaultInjectionEnabled   = "fault_injection_enabled"
    MetricFaultInjected           = "fault_injected_total"

    // Frontend, arriving through the telemetry endpoint rather than a scrape.
    MetricFrontendPageLoadSeconds = "frontend_page_load_seconds"
    MetricFrontendApiError        = "frontend_api_error_total"
    MetricFrontendBookingFunnel   = "frontend_booking_funnel_total"
    MetricFrontendCacheLookup     = "frontend_cache_lookup_total"
)

// The label names those metrics carry.
//
// Every one of them takes a value from a fixed list. That is a hard rule rather
// than a preference: a label whose values come from a request is one time series
// per caller, which leaks who did what into a system with no access control and
// runs the monitoring host out of memory on the way.
const (
    LabelReason   = "reason"
    LabelScope    = "scope"
    LabelCheck    = "check"
    LabelKind     = "kind"
    LabelName     = "name"
    LabelOutcome  = "outcome"
    LabelRoute    = "route"
    LabelMethod   = "method"
    LabelStatus   = "status"
    LabelEndpoint = "endpoint"
    LabelResult   = "result"
    LabelState    = "state"
    LabelPool     = "pool"
    LabelPoint    = "point"
    LabelStep     = "step"
    LabelCode     = "code"
)
