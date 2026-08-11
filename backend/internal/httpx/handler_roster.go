package httpx

import (
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/roster"
)

// rosterEntryResponse is one child in one seat.
type rosterEntryResponse struct {
    SeatNo      int16     `json:"seat_no"`
    StudentID   string    `json:"student_id"`
    StudentName string    `json:"student_name"`
    ConfirmedAt time.Time `json:"confirmed_at"`
}

// rosterResponse is a class and everyone who owns a seat in it.
type rosterResponse struct {
    ClassID  string                `json:"class_id"`
    Capacity int16                 `json:"capacity"`
    Entries  []rosterEntryResponse `json:"entries"`
}

// RosterHandler serves the one route that puts a child's name next to a seat.
//
// It is registered behind the admin role and nowhere else. That is the only
// thing standing between a teacher's list and a parent, so it is stated here as
// well as in the router: a change that widens this route is a change that hands
// every parent every other child's name.
type RosterHandler struct {
    rosters *roster.Service
}

// NewRosterHandler wires the route.
//
// Return:
//   - the handler
//   - roster.ErrInvalidRequest when there is no service
func NewRosterHandler(rosters *roster.Service) (*RosterHandler, error) {
    if rosters == nil {
        return nil, roster.ErrInvalidRequest
    }

    return &RosterHandler{rosters: rosters}, nil
}

// read answers who is coming to one class.
func (handler *RosterHandler) read(response http.ResponseWriter, request *http.Request) {
    seated, err := handler.rosters.For(request.Context(), request.PathValue(classIDParameter))
    if err != nil {
        Deny(response, request, err)

        return
    }

    entries := make([]rosterEntryResponse, 0, len(seated.Entries))

    for _, entry := range seated.Entries {
        entries = append(entries, rosterEntryResponse{
            SeatNo:      entry.SeatNo,
            StudentID:   entry.StudentID,
            StudentName: entry.StudentName,
            ConfirmedAt: entry.ConfirmedAt,
        })
    }

    // Never cached, and not merely private. This body carries names, and the one
    // place a copy of it must not sit is a store shared with anything else.
    writeJSON(response, http.StatusOK, noStorePolicy, rosterResponse{
        ClassID:  seated.ClassID,
        Capacity: seated.Capacity,
        Entries:  entries,
    })
}
