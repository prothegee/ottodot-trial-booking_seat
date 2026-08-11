package httpx

import (
    "net/http"

    "ottodot-trial-booking/backend/internal/auth"
)

// childResponse is one student on the account.
type childResponse struct {
    ID         string `json:"id"`
    FullName   string `json:"full_name"`
    GradeLevel int16  `json:"grade_level"`
}

// studentListResponse wraps the list in an object rather than sending a bare
// array. An object can grow a field, an array cannot.
type studentListResponse struct {
    Children []childResponse `json:"children"`
}

// StudentHandler answers which children are on the caller's account.
//
// It reads the token subject and never a parameter. There is no route in this
// api that lists somebody else's children, which is why this handler takes no
// identifier at all: a route with no way to name another parent is a route that
// cannot be pointed at one.
type StudentHandler struct {
    directory auth.Directory
}

// NewStudentHandler wires the route.
//
// Return:
//   - the handler
//   - auth.ErrInvalidRequest when there is no directory
func NewStudentHandler(directory auth.Directory) (*StudentHandler, error) {
    if directory == nil {
        return nil, auth.ErrInvalidRequest
    }

    return &StudentHandler{directory: directory}, nil
}

// list answers the children on the caller's account.
func (handler *StudentHandler) list(response http.ResponseWriter, request *http.Request) {
    identity, carried := identityOf(response, request)
    if !carried {
        return
    }

    account, err := handler.directory.Account(request.Context(), identity.ParentID)
    if err != nil {
        Deny(response, request, err)

        return
    }

    children := make([]childResponse, 0, len(account.Children))

    for _, child := range account.Children {
        children = append(children, childResponse{
            ID:         child.ID,
            FullName:   child.FullName,
            GradeLevel: child.GradeLevel,
        })
    }

    // Private and short lived. This body carries children's names, so no shared
    // proxy may keep a copy, and the few seconds are only there so one screen
    // loading twice does not read twice.
    writeJSON(response, http.StatusOK, privateReadPolicy, studentListResponse{Children: children})
}
