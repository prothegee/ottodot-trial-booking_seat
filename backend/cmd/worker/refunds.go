package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/observability"
    "ottodot-trial-booking/backend/internal/payment"
)

// refundLine is what gets written down when money goes back.
//
// A refund has no table of its own, so this line is where the reference lives.
// It carries the attempt and the two provider references, and nothing about a
// person: no parent, no child, no class, and no amount. What somebody paid is not
// needed to trace a refund, and the attempt row already holds it.
//
// It is a pure function so the rule above can be tested without a logger, a
// queue, or a refund actually happening.
//
// Param:
// refund - payment.Refund (the settled refund)
//
// Return:
//   - one line naming the attempt and both provider references
func refundLine(refund payment.Refund) string {
    return fmt.Sprintf("refund settled, attempt %s, charge %s, refund %s",
        refund.AttemptID, refund.ProviderRef, refund.RefundRef)
}

// refundRecorder builds the callback the reconciliation handler is given.
//
// It is a closure over the logger rather than a package level function reading a
// package level logger, because the handler takes a plain function and a
// mutable global would be a second way for this to be nil at the wrong moment.
//
// Param:
// logger - *observability.Logger (where the line goes)
//
// Return:
//   - the callback, safe to hand to the handler
func refundRecorder(logger *observability.Logger) func(payment.Refund) {
    return func(refund payment.Refund) {
        logger.Info(refundLine(refund))
    }
}
