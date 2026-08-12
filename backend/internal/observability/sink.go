package observability

// The methods below let *Metrics stand in for the narrow recording interfaces
// the other packages declare for themselves.
//
// They exist because the four groups are separate types, and a package that
// records one access denial and one request duration would otherwise have to be
// handed two objects and keep them in step. Forwarding is the cost of that, and
// it is paid once here rather than at every wiring site.
//
// Nothing in this file decides anything. Each method is the group's own method
// with the group looked up, and the nil safety comes from the group rather than
// from a check repeated here.

// AccessDenied forwards one refusal.
func (metrics *Metrics) AccessDenied(reason string) {
    if metrics == nil {
        return
    }

    metrics.Access.AccessDenied(reason)
}

// RateLimitRejected forwards one caller turned away by a bucket.
func (metrics *Metrics) RateLimitRejected(scope string) {
    if metrics == nil {
        return
    }

    metrics.Access.RateLimitRejected(scope)
}

// BotCheckRejected forwards one submission the cooperative checks refused.
func (metrics *Metrics) BotCheckRejected(check string) {
    if metrics == nil {
        return
    }

    metrics.Access.BotCheckRejected(check)
}

// TokenIssued forwards one token handed out.
func (metrics *Metrics) TokenIssued(kind string) {
    if metrics == nil {
        return
    }

    metrics.Access.TokenIssued(kind)
}

// RefreshReuseDetected forwards one replayed refresh token.
func (metrics *Metrics) RefreshReuseDetected() {
    if metrics == nil {
        return
    }

    metrics.Access.RefreshReuseDetected()
}

// RefreshRotated forwards one refresh exchanged for a successor.
func (metrics *Metrics) RefreshRotated() {
    if metrics == nil {
        return
    }

    metrics.Access.RefreshRotated()
}

// LoginRefused forwards one sign in that was not accepted.
func (metrics *Metrics) LoginRefused() {
    if metrics == nil {
        return
    }

    metrics.Access.LoginRefused()
}

// RequestObserved forwards one served request.
func (metrics *Metrics) RequestObserved(route string, method string, status int, seconds float64) {
    if metrics == nil {
        return
    }

    metrics.Application.RequestObserved(route, method, status, seconds)
}

// DeclareRoute forwards one registered route, so its series exists at zero.
func (metrics *Metrics) DeclareRoute(route string, method string) {
    if metrics == nil {
        return
    }

    metrics.Application.DeclareRoute(route, method)
}

// NotModified forwards one conditional read answered with no body.
func (metrics *Metrics) NotModified(route string) {
    if metrics == nil {
        return
    }

    metrics.Application.NotModified(route)
}

// PanicRecovered forwards one handler that fell over.
func (metrics *Metrics) PanicRecovered() {
    if metrics == nil {
        return
    }

    metrics.Application.PanicRecovered()
}

// CacheLookup forwards one response cache read.
func (metrics *Metrics) CacheLookup(endpoint string, result string) {
    if metrics == nil {
        return
    }

    metrics.Application.CacheLookup(endpoint, result)
}

// HoldGranted forwards one hold request and its outcome.
func (metrics *Metrics) HoldGranted(outcome string) {
    if metrics == nil {
        return
    }

    metrics.Application.HoldGranted(outcome)
}

// BookingConfirmed forwards one seat taken.
func (metrics *Metrics) BookingConfirmed() {
    if metrics == nil {
        return
    }

    metrics.Application.BookingConfirmed()
}

// RaceLost forwards one parent who reached the last seat second.
func (metrics *Metrics) RaceLost() {
    if metrics == nil {
        return
    }

    metrics.Application.RaceLost()
}

// DuplicateRejected forwards one booking refused as a repeat.
func (metrics *Metrics) DuplicateRejected() {
    if metrics == nil {
        return
    }

    metrics.Application.DuplicateRejected()
}

// HoldExpired forwards one hold the worker released.
func (metrics *Metrics) HoldExpired() {
    if metrics == nil {
        return
    }

    metrics.Application.HoldExpired()
}

// QueueDepth forwards what the queue holds right now.
func (metrics *Metrics) QueueDepth(ready int, claimed int, parked int) {
    if metrics == nil {
        return
    }

    metrics.Application.QueueDepth(ready, claimed, parked)
}

// QueueDepthUnknown forwards that the queue could not be asked how deep it is.
func (metrics *Metrics) QueueDepthUnknown() {
    if metrics == nil {
        return
    }

    metrics.Application.QueueDepthUnknown()
}

// JobsClaimed forwards jobs handed to a worker.
func (metrics *Metrics) JobsClaimed(jobs int) {
    if metrics == nil {
        return
    }

    metrics.Application.JobsClaimed(jobs)
}

// JobsCompleted forwards one job that finished and was removed.
func (metrics *Metrics) JobsCompleted() {
    if metrics == nil {
        return
    }

    metrics.Application.JobsCompleted()
}

// QueueJobFailed forwards one job attempt that did not finish.
func (metrics *Metrics) QueueJobFailed(kind string) {
    if metrics == nil {
        return
    }

    metrics.Transaction.QueueJobFailed(kind)
}

// QueueJobFinished forwards one job attempt and how long it took.
func (metrics *Metrics) QueueJobFinished(kind string, outcome string, seconds float64) {
    if metrics == nil {
        return
    }

    metrics.Application.QueueJob(kind, outcome, seconds)
}

// ConfirmTransaction forwards one seat confirmation attempt and its duration.
func (metrics *Metrics) ConfirmTransaction(outcome string, seconds float64) {
    if metrics == nil {
        return
    }

    metrics.Transaction.ConfirmTransaction(outcome, seconds)
}

// PaymentAttempt forwards one charge and how it ended.
func (metrics *Metrics) PaymentAttempt(outcome string) {
    if metrics == nil {
        return
    }

    metrics.Transaction.PaymentAttempt(outcome)
}

// DatabaseTransaction forwards one transaction and how it ended.
func (metrics *Metrics) DatabaseTransaction(name string, outcome string) {
    if metrics == nil {
        return
    }

    metrics.Transaction.DatabaseTransaction(name, outcome)
}

// FaultInjected forwards one fault that actually fired.
func (metrics *Metrics) FaultInjected(point string) {
    if metrics == nil {
        return
    }

    metrics.Application.FaultInjected(point)
}
