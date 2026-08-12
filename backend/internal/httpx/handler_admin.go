package httpx

import (
    "net/http"
    "strconv"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/queue"
)

// The bounds on the operator worklist.
//
// A cap is required rather than defaulted at the repository, and it is also
// clamped here, so a screen asking for everything gets a page instead of the
// table.
const (
    defaultWorklistLimit = 50
    maximumWorklistLimit = 200
)

// worklistMaxAttempts is the number the queue depth is measured against. It has
// to be the number the runner claims with, or the parked count on this screen
// disagrees with the one the worker acts on.
const worklistMaxAttempts = 5

// queueDepthResponse is the queue in the three numbers that mean different
// things.
//
// Ready rising means the worker is behind. Claimed rising means jobs are slow.
// Parked rising means something is broken and no amount of waiting fixes it.
type queueDepthResponse struct {
    Ready   int `json:"ready"`
    Claimed int `json:"claimed"`
    Parked  int `json:"parked"`
}

// adminBookingListResponse is the operator worklist.
type adminBookingListResponse struct {
    Bookings []bookingResponse `json:"bookings"`
}

// AdminHandler serves the two operator reads.
//
// Both are registered behind the admin role. Neither writes anything: an
// operator surface that could change a booking would need an audit story of its
// own, and phase 6 is not where that belongs.
type AdminHandler struct {
    bookings   *booking.Service
    jobs       queue.Queue
    classNames *ClassNames
    clock      func() time.Time
}

// NewAdminHandler wires the routes.
//
// Return:
//   - the handler
//   - booking.ErrInvalidRequest when a collaborator is missing
func NewAdminHandler(bookings *booking.Service, jobs queue.Queue, classNames *ClassNames, clock func() time.Time) (*AdminHandler, error) {
    if bookings == nil || jobs == nil || classNames == nil {
        return nil, booking.ErrInvalidRequest
    }

    if clock == nil {
        clock = time.Now
    }

    return &AdminHandler{bookings: bookings, jobs: jobs, classNames: classNames, clock: clock}, nil
}

// queueDepth answers what the job queue is holding.
func (handler *AdminHandler) queueDepth(response http.ResponseWriter, request *http.Request) {
    depth, err := handler.jobs.Depth(request.Context(), queue.DepthRequest{
        Now:         handler.clock(),
        MaxAttempts: worklistMaxAttempts,
    })
    if err != nil {
        Deny(response, request, err)

        return
    }

    writeJSON(response, http.StatusOK, noStorePolicy, queueDepthResponse{
        Ready:   depth.Ready,
        Claimed: depth.Claimed,
        Parked:  depth.Parked,
    })
}

// worklist answers the bookings an operator is looking at.
//
// The status filter is read from the query string and refused when it is not one
// this service has. Passing an unknown value through would produce an empty list
// that reads as "nothing to do" rather than as "you asked for something that
// does not exist".
func (handler *AdminHandler) worklist(response http.ResponseWriter, request *http.Request) {
    status := booking.Status(request.URL.Query().Get("status"))

    if status != "" && !status.IsKnown() {
        Deny(response, request, booking.ErrInvalidRequest)

        return
    }

    limit, err := worklistLimit(request.URL.Query().Get("limit"))
    if err != nil {
        Deny(response, request, err)

        return
    }

    listed, err := handler.bookings.Worklist(request.Context(), status, limit)
    if err != nil {
        Deny(response, request, err)

        return
    }

    writeJSON(response, http.StatusOK, noStorePolicy, adminBookingListResponse{
        Bookings: bookingsFrom(listed, handler.classNames.ForAll(request.Context(), listed)),
    })
}

// worklistLimit reads the page size, or refuses one that cannot be honoured.
//
// An absent value is the default rather than an error, so the screen opens with
// no parameters. A value above the maximum is refused rather than quietly
// lowered: an operator who asked for a thousand rows and silently got two
// hundred would read the short list as the whole answer.
func worklistLimit(raw string) (int, error) {
    if raw == "" {
        return defaultWorklistLimit, nil
    }

    asked, err := strconv.Atoi(raw)
    if err != nil {
        return 0, booking.ErrInvalidRequest
    }

    if asked < 1 || asked > maximumWorklistLimit {
        return 0, booking.ErrInvalidRequest
    }

    return asked, nil
}
