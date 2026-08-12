package httpx

import (
    "errors"
    "net/http"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/payment"
)

// payRequest is the payment body.
//
// The amount is sent by the client and checked against the price this service
// owns before anything is charged. It is on the wire at all so a client that
// believes it is paying something else is told, rather than charged the right
// amount for the wrong reason.
type payRequest struct {
    AmountCents int32  `json:"amount_cents"`
    Currency    string `json:"currency"`

    Signals
}

// PaymentHandler serves the one route where money moves.
type PaymentHandler struct {
    checkout    *checkout.Service
    owner       *Owner
    botCheck    *BotCheck
    conditional *Conditional
    classNames  *ClassNames

    // development relaxes the amount check to the two values the mock provider
    // reads as a decline and as an unreachable provider. It is false everywhere
    // but a local run.
    development bool
}

// NewPaymentHandler wires the route.
//
// Param:
// checkoutService - *checkout.Service (the order money and seats happen in)
// owner - *Owner (whether this booking is the caller's)
// botCheck - *BotCheck (the cooperative checks)
// conditional - *Conditional (what to invalidate when a seat changes hands)
// classNames - *ClassNames (what the booking it answers with was for)
// development - bool (whether the two demonstration amounts are accepted)
//
// Return:
//   - the handler
//   - payment.ErrInvalidRequest when a collaborator is missing
func NewPaymentHandler(checkoutService *checkout.Service, owner *Owner, botCheck *BotCheck, conditional *Conditional, classNames *ClassNames, development bool) (*PaymentHandler, error) {
    if checkoutService == nil || owner == nil || botCheck == nil || conditional == nil || classNames == nil {
        return nil, payment.ErrInvalidRequest
    }

    return &PaymentHandler{
        checkout:    checkoutService,
        owner:       owner,
        botCheck:    botCheck,
        conditional: conditional,
        classNames:  classNames,
        development: development,
    }, nil
}

// pay settles the charge and then decides the seat.
//
// Every refusal above the checkout call happens before a provider is contacted,
// which is the property that matters on this route: a request that was never
// going to be honoured must not cost anybody money on its way to being refused.
//
// Three outcomes are not failures of this service and are answered as business
// results, with the booking attached so the client can render the truth rather
// than guess it:
//
//	402 payment_declined     no money moved, the booking finished
//	409 seat_lost            money moved and is being returned, the booking is
//	                         refund_required and a job is queued
//	503 dependency_unavailable   nobody knows whether money moved, so the
//	                         booking keeps the status it had
func (handler *PaymentHandler) pay(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    var body payRequest

    if err := decodeBody(request, &body); err != nil {
        Deny(response, request, err)

        return
    }

    key := request.Header.Get(IdempotencyKeyHeader)

    if err := payment.ValidateIdempotencyKey(key); err != nil {
        Deny(response, request, err)

        return
    }

    stored, err := handler.owner.Booking(request.Context(), identity, request.PathValue(bookingIDParameter))
    if err != nil {
        Deny(response, request, err)

        return
    }

    if err := handler.botCheck.Inspect(request.Context(), body.Signals, CallerAddress(request)); err != nil {
        Deny(response, request, err)

        return
    }

    result, err := handler.checkout.Pay(request.Context(), checkout.PayCommand{
        BookingID:      stored.ID,
        Amount:         payment.Amount{Cents: body.AmountCents, Currency: body.Currency},
        IdempotencyKey: key,
        Development:    handler.development,
    })

    if err != nil {
        handler.denyPayment(response, request, stored.ClassID, err)

        return
    }

    // A confirmed seat changes what every other parent sees, so both cached
    // documents are invalidated before the answer goes out.
    handler.conditional.Invalidate(request.Context(), cache.ClassListKey(), cache.ClassKey(stored.ClassID))

    writeJSON(response, http.StatusOK, noStorePolicy,
        bookingFrom(result.Booking, handler.classNames.For(request.Context(), result.Booking)))
}

// denyPayment answers a checkout that did not end in a seat.
//
// A decline and a lost seat both changed the booking, so both invalidate the
// cached seat counts on the way out. An unreachable provider changed nothing, so
// it invalidates nothing: dropping a cached body because a third party timed out
// would cost every other parent a database read for no reason.
//
// No booking id is attached. The client is already on that booking's screen, and
// the only refusal in this api that names one is the duplicate, where the parent
// genuinely does not know which booking is meant.
func (handler *PaymentHandler) denyPayment(response http.ResponseWriter, request *http.Request, classID string, err error) {
    if errors.Is(err, payment.ErrDeclined) || errors.Is(err, booking.ErrSeatLost) {
        handler.conditional.Invalidate(request.Context(), cache.ClassListKey(), cache.ClassKey(classID))
    }

    Deny(response, request, err)
}
