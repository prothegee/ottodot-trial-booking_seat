package catalogue

import (
    "context"
    "errors"
    "strings"
)

// Service is what the http layer talks to.
//
// It owns two things and no more: refusing a read that names nothing, and
// counting how many reads actually reached storage. The second is not
// bookkeeping for its own sake, it is what the conditional request test
// asserts against, because "the database was not touched" has to be provable
// rather than asserted.
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
        return nil, errors.New("catalogue: the service needs a reader")
    }

    return &Service{reader: reader}, nil
}

// Classes lists every trial class with the seats it has left, soonest first.
//
// The slice is never nil. A catalogue with nothing in it answers with an empty
// list, so a client renders "no classes" instead of guarding against null.
func (service *Service) Classes(ctx context.Context) ([]Class, error) {
    listed, err := service.reader.Classes(ctx)
    if err != nil {
        return nil, err
    }

    if listed == nil {
        return []Class{}, nil
    }

    return listed, nil
}

// Class reads one class.
//
// Return:
//   - the class with its advisory seat count
//   - ErrInvalidRequest when nothing was named
//   - ErrClassNotFound when there is no such class
func (service *Service) Class(ctx context.Context, classID string) (Class, error) {
    if strings.TrimSpace(classID) == "" {
        return Class{}, ErrInvalidRequest
    }

    return service.reader.Class(ctx, classID)
}
