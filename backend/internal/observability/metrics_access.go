package observability

import "github.com/prometheus/client_golang/prometheus"

// The values the access failure labels are allowed to take.
//
// They are constants and not free text, and the difference matters. A denial
// reason taken from an error string would put the error's wording into a label,
// and error wording changes, which silently splits one series into two.
const (
    ReasonTokenExpired  = "token_expired"
    ReasonTokenInvalid  = "token_invalid"
    ReasonTokenReused   = "token_reused"
    ReasonNotYourChild  = "not_your_child"
    ReasonForbiddenRole = "forbidden_role"

    // ReasonOriginRefused is a write that did not come from this service's own
    // page. It is not in the monitoring plan's original list, and it is here
    // because leaving it out would have folded it into token_invalid, where a
    // spike of cross site attempts would look exactly like a batch of sessions
    // timing out.
    ReasonOriginRefused = "origin_refused"

    ScopeSubject = "subject"
    ScopeAddress = "ip"

    CheckHoneypot = "honeypot"
    CheckFillTime = "fill_time"
    CheckCaptcha  = "captcha"

    TokenKindAccess  = "access"
    TokenKindRefresh = "refresh"
)

// AccessMetrics is what this service refused, and why.
//
// They are a group of their own rather than rows in a status code chart because
// the brief calls access failures out as important. A 403 buried among every
// other four hundred is a number nobody looks at, and the question an operator
// actually has is whether refusals are a broken client, an expired batch of
// tokens, or somebody trying accounts one at a time. The reason label is what
// tells those three apart.
type AccessMetrics struct {
    denied        *prometheus.CounterVec
    rateLimited   *prometheus.CounterVec
    botRejected   *prometheus.CounterVec
    tokensIssued  *prometheus.CounterVec
    refreshReuse  prometheus.Counter
    refreshRotate prometheus.Counter
    loginRefused  prometheus.Counter
}

// newAccessMetrics builds the group and registers it.
func newAccessMetrics(registry prometheus.Registerer) *AccessMetrics {
    metrics := &AccessMetrics{
        denied: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricAccessDenied,
            Help: "Requests refused for who the caller was rather than for what they asked.",
        }, []string{LabelReason}),

        rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricRateLimitRejected,
            Help: "Requests turned away by a token bucket, by which bucket ran dry.",
        }, []string{LabelScope}),

        botRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricBotCheckRejected,
            Help: "Submissions the cooperative bot checks turned away, by which check caught it.",
        }, []string{LabelCheck}),

        tokensIssued: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricAuthTokenIssued,
            Help: "Tokens handed out, by kind.",
        }, []string{LabelKind}),

        refreshReuse: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricAuthRefreshReuse,
            Help: "Spent refresh tokens presented a second time, which revokes the whole family.",
        }),

        refreshRotate: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricAuthRefreshRotated,
            Help: "Refresh tokens exchanged for a successor.",
        }),

        loginRefused: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricAuthLoginRefused,
            Help: "Sign in attempts that matched no account or the wrong secret.",
        }),
    }

    registry.MustRegister(
        metrics.denied,
        metrics.rateLimited,
        metrics.botRejected,
        metrics.tokensIssued,
        metrics.refreshReuse,
        metrics.refreshRotate,
        metrics.loginRefused)

    // Every series is created at zero before anything happens. A counter that
    // has never been incremented is absent from the exposition entirely, and an
    // absent series makes `rate()` return nothing rather than zero, so a panel
    // reads empty on a healthy service and an alert cannot tell "no denials"
    // from "the metric was renamed".
    for _, reason := range []string{ReasonTokenExpired, ReasonTokenInvalid, ReasonTokenReused, ReasonNotYourChild, ReasonForbiddenRole, ReasonOriginRefused} {
        metrics.denied.WithLabelValues(reason)
    }

    for _, scope := range []string{ScopeSubject, ScopeAddress} {
        metrics.rateLimited.WithLabelValues(scope)
    }

    for _, check := range []string{CheckHoneypot, CheckFillTime, CheckCaptcha} {
        metrics.botRejected.WithLabelValues(check)
    }

    for _, kind := range []string{TokenKindAccess, TokenKindRefresh} {
        metrics.tokensIssued.WithLabelValues(kind)
    }

    return metrics
}

// AccessDenied records one refusal. An unknown reason is folded into
// token_invalid rather than opening a new series, because the caller of this
// method is the only thing that could produce one and a typo there must not
// become a permanent label value.
func (metrics *AccessMetrics) AccessDenied(reason string) {
    if metrics == nil {
        return
    }

    metrics.denied.WithLabelValues(knownReason(reason)).Inc()
}

// RateLimitRejected records one caller turned away by a bucket.
func (metrics *AccessMetrics) RateLimitRejected(scope string) {
    if metrics == nil {
        return
    }

    if scope != ScopeSubject {
        scope = ScopeAddress
    }

    metrics.rateLimited.WithLabelValues(scope).Inc()
}

// BotCheckRejected records one submission the cooperative checks refused.
func (metrics *AccessMetrics) BotCheckRejected(check string) {
    if metrics == nil {
        return
    }

    switch check {
    case CheckHoneypot, CheckFillTime, CheckCaptcha:
    default:
        check = CheckHoneypot
    }

    metrics.botRejected.WithLabelValues(check).Inc()
}

// TokenIssued records one token handed out.
func (metrics *AccessMetrics) TokenIssued(kind string) {
    if metrics == nil {
        return
    }

    if kind != TokenKindRefresh {
        kind = TokenKindAccess
    }

    metrics.tokensIssued.WithLabelValues(kind).Inc()
}

// RefreshReuseDetected records one stolen or replayed refresh token. It is the
// single number on this whole surface that always deserves a person's attention,
// which is why it has an alert of its own on any increase at all.
func (metrics *AccessMetrics) RefreshReuseDetected() {
    if metrics == nil {
        return
    }

    metrics.refreshReuse.Inc()
}

// RefreshRotated records one refresh exchanged for a successor.
func (metrics *AccessMetrics) RefreshRotated() {
    if metrics == nil {
        return
    }

    metrics.refreshRotate.Inc()
}

// LoginRefused records one sign in that was not accepted.
func (metrics *AccessMetrics) LoginRefused() {
    if metrics == nil {
        return
    }

    metrics.loginRefused.Inc()
}

// knownReason keeps the reason label inside its fixed list.
func knownReason(reason string) string {
    switch reason {
    case ReasonTokenExpired, ReasonTokenInvalid, ReasonTokenReused, ReasonNotYourChild, ReasonForbiddenRole, ReasonOriginRefused:
        return reason
    default:
        return ReasonTokenInvalid
    }
}
