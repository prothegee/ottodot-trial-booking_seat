package httpx

import (
    "context"
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/catalogue"
)

// classResponse is one trial class as a parent's screen reads it.
//
// seats_remaining is advisory and the client is told so in its own types. It is
// what saves a parent a wasted click, and by the time they click it may already
// be wrong, which every screen is built to handle.
type classResponse struct {
    ID              string    `json:"id"`
    Subject         string    `json:"subject"`
    Title           string    `json:"title"`
    StartsAt        time.Time `json:"starts_at"`
    DurationMinutes int16     `json:"duration_minutes"`
    Capacity        int16     `json:"capacity"`
    SeatsRemaining  int16     `json:"seats_remaining"`
}

// classListResponse wraps the list in an object rather than sending a bare
// array. An object can grow a field, an array cannot.
type classListResponse struct {
    Classes []classResponse `json:"classes"`
}

// ClassHandler serves the two cacheable reads.
type ClassHandler struct {
    classes     *catalogue.Service
    conditional *Conditional
}

// NewClassHandler wires the routes.
//
// Return:
//   - the handler
//   - catalogue.ErrInvalidRequest when a collaborator is missing
func NewClassHandler(classes *catalogue.Service, conditional *Conditional) (*ClassHandler, error) {
    if classes == nil || conditional == nil {
        return nil, catalogue.ErrInvalidRequest
    }

    return &ClassHandler{classes: classes, conditional: conditional}, nil
}

// list answers the class list, from the cache whenever it can.
func (handler *ClassHandler) list(response http.ResponseWriter, request *http.Request) {
    handler.conditional.Serve(response, request, cache.ClassListKey(), cacheableReadPolicy,
        func(ctx context.Context) (any, error) {
            listed, err := handler.classes.Classes(ctx)
            if err != nil {
                return nil, err
            }

            return classListResponse{Classes: classesFrom(listed)}, nil
        })
}

// one answers a single class, from the cache whenever it can.
func (handler *ClassHandler) one(response http.ResponseWriter, request *http.Request) {
    classID := request.PathValue(classIDParameter)

    key := cache.ClassKey(classID)
    if key == "" {
        Deny(response, request, catalogue.ErrInvalidRequest)

        return
    }

    handler.conditional.Serve(response, request, key, cacheableReadPolicy,
        func(ctx context.Context) (any, error) {
            class, err := handler.classes.Class(ctx, classID)
            if err != nil {
                return nil, err
            }

            return classFrom(class), nil
        })
}

// classFrom shapes one class for the wire.
func classFrom(class catalogue.Class) classResponse {
    return classResponse{
        ID:              class.ID,
        Subject:         class.Subject,
        Title:           class.Title,
        StartsAt:        class.StartsAt,
        DurationMinutes: class.DurationMinutes,
        Capacity:        class.Capacity,
        SeatsRemaining:  class.SeatsRemaining,
    }
}

// classesFrom shapes a list, empty rather than nil so the client renders "no
// classes" instead of guarding against null.
func classesFrom(listed []catalogue.Class) []classResponse {
    shaped := make([]classResponse, 0, len(listed))

    for _, class := range listed {
        shaped = append(shaped, classFrom(class))
    }

    return shaped
}
