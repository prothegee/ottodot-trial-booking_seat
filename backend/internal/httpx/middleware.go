package httpx

import "net/http"

// Middleware is one wrapper around a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in the order it is written.
//
// `Chain(handler, first, second)` runs first, then second, then the handler,
// which is the order a reader expects from the order on the page. Composing them
// the other way round is the mistake this function exists to make impossible,
// and it is not a small one: a chain that authenticated after rate limiting
// would count every request against an address instead of an account.
//
// Param:
// handler - http.Handler (what runs at the end)
// middleware - ...Middleware (outermost first)
//
// Return:
//   - the wrapped handler
func Chain(handler http.Handler, middleware ...Middleware) http.Handler {
    for index := len(middleware) - 1; index >= 0; index-- {
        handler = middleware[index](handler)
    }

    return handler
}
