package httpx

import (
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/payment"
)

// maximumBodyBytes caps what a request body may be.
//
// The largest body any route here reads is two identifiers and three bot
// signals, so anything above this is either a mistake or somebody seeing how
// much memory a request can be made to hold.
const maximumBodyBytes = 4 * 1024

// createBookingRequest is the hold request.
//
// The bot signals are embedded rather than nested, so the wire shape stays flat
// and the honeypot looks like an ordinary field to whatever is filling it in.
type createBookingRequest struct {
    StudentID string `json:"student_id"`
    ClassID   string `json:"class_id"`

    Signals
}

// eventResponse is one line of the audit trail.
//
// The reason is included and the actor is named, because that is the whole value
// of the trail. Neither ever carries an identifier or a name: every reason in
// this service is written by the service itself, for exactly this reason.
type eventResponse struct {
    ID         string    `json:"id"`
    FromStatus string    `json:"from_status"`
    ToStatus   string    `json:"to_status"`
    Actor      string    `json:"actor"`
    Reason     string    `json:"reason"`
    CreatedAt  time.Time `json:"created_at"`
}

// eventListResponse wraps the trail in an object rather than sending a bare
// array.
type eventListResponse struct {
    Events []eventResponse `json:"events"`
}

// bookingListResponse is the parent's own bookings.
type bookingListResponse struct {
    Bookings []bookingResponse `json:"bookings"`
}

// parentBookingsLimit caps the parent's own list.
//
// It is fixed rather than a query parameter. A family has a handful of bookings
// and a screen that showed all of them either way, so a page size would be a
// knob with nothing behind it and one more thing to validate. The cap is here so
// the read cannot grow with an account that has been used for years.
const parentBookingsLimit = 50

// BookingHandler serves the five booking routes.
type BookingHandler struct {
    checkout    *checkout.Service
    bookings    *booking.Service
    owner       *Owner
    botCheck    *BotCheck
    conditional *Conditional
    classNames  *ClassNames
}

// NewBookingHandler wires the routes.
//
// Return:
//   - the handler
//   - booking.ErrInvalidRequest when a collaborator is missing
func NewBookingHandler(checkoutService *checkout.Service, bookings *booking.Service, owner *Owner, botCheck *BotCheck, conditional *Conditional, classNames *ClassNames) (*BookingHandler, error) {
    if checkoutService == nil || bookings == nil || owner == nil || botCheck == nil || conditional == nil || classNames == nil {
        return nil, booking.ErrInvalidRequest
    }

    return &BookingHandler{
        checkout:    checkoutService,
        bookings:    bookings,
        owner:       owner,
        botCheck:    botCheck,
        conditional: conditional,
        classNames:  classNames,
    }, nil
}

// create asks for a place on the payment screen.
//
// The order of the checks is the order of cost, and it is the order test 10
// walks: who the caller is, then whether the child is theirs, then whether
// the submission looks like a person, and only then the transaction. Everything
// above the transaction costs almost nothing, and the transaction is the thing
// worth protecting.
//
// The idempotency key is validated but is not what makes a double click safe
// here. The unique index on student and class is: a second live booking for the
// same child cannot exist, so the second click is answered with the booking the
// first one made. The key does its real work on the payment route, where it is
// the only thing standing between a retry and a second charge.
func (handler *BookingHandler) create(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    var body createBookingRequest

    if err := decodeBody(request, &body); err != nil {
        Deny(response, request, err)

        return
    }

    if err := payment.ValidateIdempotencyKey(request.Header.Get(IdempotencyKeyHeader)); err != nil {
        Deny(response, request, err)

        return
    }

    if err := handler.owner.Student(request.Context(), identity, body.StudentID); err != nil {
        Deny(response, request, err)

        return
    }

    if err := handler.botCheck.Inspect(request.Context(), body.Signals, CallerAddress(request)); err != nil {
        Deny(response, request, err)

        return
    }

    granted, err := handler.checkout.Hold(request.Context(), booking.HoldCommand{
        StudentID: body.StudentID,
        ClassID:   body.ClassID,
    })
    if err != nil {
        handler.denyHold(response, request, body, err)

        return
    }

    handler.conditional.Invalidate(request.Context(), cache.ClassListKey(), cache.ClassKey(body.ClassID))

    writeJSON(response, http.StatusCreated, noStorePolicy,
        bookingFrom(granted.Booking, handler.classNames.For(request.Context(), granted.Booking)))
}

// denyHold answers a refused hold, naming the existing booking when the reason
// is that there already is one.
//
// Looking it up costs one read on a path that already failed, and it is what
// turns "you already booked this" into a notice the parent can act on rather
// than one that sends them looking.
func (handler *BookingHandler) denyHold(response http.ResponseWriter, request *http.Request, body createBookingRequest, err error) {
    failure := FailureFor(err)

    if errors.Is(err, booking.ErrAlreadyBooked) {
        if existing, found := handler.bookings.LiveBooking(request.Context(), body.StudentID, body.ClassID); found == nil {
            failure.BookingID = existing.ID
        }
    }

    denyWith(response, request, failure, err)
}

// read answers one booking's status.
//
// It is never cached. A status is what a parent checks straight after paying, so
// it goes to the primary and it is read fresh: seeing a stale pending_payment
// after a card cleared looks exactly like the payment was lost.
func (handler *BookingHandler) read(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    stored, err := handler.owner.Booking(request.Context(), identity, request.PathValue(bookingIDParameter))
    if err != nil {
        Deny(response, request, err)

        return
    }

    writeJSON(response, http.StatusOK, noStorePolicy,
        bookingFrom(stored, handler.classNames.For(request.Context(), stored)))
}

// list answers the bookings belonging to whoever is signed in.
//
// It exists because every other way back to a booking is the address it was
// created at, which a closed tab takes with it.
//
// The ownership check the other routes run is not skipped, it is not needed: the
// parent is the query. Nothing is read from the request, so this route cannot be
// asked about somebody else. Never cached, for the reason read gives above.
func (handler *BookingHandler) list(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    listed, err := handler.bookings.ParentBookings(request.Context(), identity.ParentID, parentBookingsLimit)
    if err != nil {
        Deny(response, request, err)

        return
    }

    writeJSON(response, http.StatusOK, noStorePolicy, bookingListResponse{
        Bookings: bookingsFrom(listed, handler.classNames.ForAll(request.Context(), listed)),
    })
}

// cancel withdraws a live hold and frees whatever it was holding.
func (handler *BookingHandler) cancel(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    stored, err := handler.owner.Booking(request.Context(), identity, request.PathValue(bookingIDParameter))
    if err != nil {
        Deny(response, request, err)

        return
    }

    withdrawn, err := handler.bookings.Cancel(request.Context(), stored.ID, booking.ActorParent, "withdrawn by the parent")
    if err != nil {
        Deny(response, request, err)

        return
    }

    handler.conditional.Invalidate(request.Context(), cache.ClassListKey(), cache.ClassKey(stored.ClassID))

    writeJSON(response, http.StatusOK, noStorePolicy,
        bookingFrom(withdrawn, handler.classNames.For(request.Context(), withdrawn)))
}

// events answers the audit trail for one booking.
func (handler *BookingHandler) events(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    stored, err := handler.owner.Booking(request.Context(), identity, request.PathValue(bookingIDParameter))
    if err != nil {
        Deny(response, request, err)

        return
    }

    trail, err := handler.bookings.Events(request.Context(), stored.ID)
    if err != nil {
        Deny(response, request, err)

        return
    }

    shaped := make([]eventResponse, 0, len(trail))

    for _, entry := range trail {
        shaped = append(shaped, eventResponse{
            ID:         entry.ID,
            FromStatus: string(entry.FromStatus),
            ToStatus:   string(entry.ToStatus),
            Actor:      string(entry.Actor),
            Reason:     entry.Reason,
            CreatedAt:  entry.CreatedAt,
        })
    }

    writeJSON(response, http.StatusOK, noStorePolicy, eventListResponse{Events: shaped})
}

// decodeBody reads one json body, capped and strict.
//
// Unknown fields are refused rather than ignored. A client sending a field this
// service does not have should be told, not quietly served as though the field
// had been honoured.
func decodeBody(request *http.Request, into any) error {
    decoder := json.NewDecoder(io.LimitReader(request.Body, maximumBodyBytes))
    decoder.DisallowUnknownFields()

    if err := decoder.Decode(into); err != nil {
        return booking.ErrInvalidRequest
    }

    return nil
}
