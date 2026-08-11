package roster

import (
    "context"
    "errors"
    "strings"
)

// Service is what the http layer talks to.
//
// It owns refusing a read that names nothing, and nothing else. In particular it
// does not check who is asking: that is the middleware's job, and a service that
// both listed children and decided who may see them would be two
// responsibilities in one method, which is exactly the shape where one of them
// gets skipped.
type Service struct {
    reader Reader
}

// NewService wraps a reader.
//
// Param:
// reader - Reader (the replica backed one in production, the fake in the fast tiers)
//
// Return:
//   - the service
//   - an error when there is no reader, refused at construction rather than at
//     the first request
func NewService(reader Reader) (*Service, error) {
    if reader == nil {
        return nil, errors.New("roster: the service needs a reader")
    }

    return &Service{reader: reader}, nil
}

// For reads everyone who owns a seat in one class.
//
// Return:
//   - the roster, with an empty entry list for a class nobody has booked
//   - ErrInvalidRequest when nothing was named
//   - ErrClassNotFound when there is no such class
func (service *Service) For(ctx context.Context, classID string) (Roster, error) {
    if strings.TrimSpace(classID) == "" {
        return Roster{}, ErrInvalidRequest
    }

    return service.reader.For(ctx, classID)
}
