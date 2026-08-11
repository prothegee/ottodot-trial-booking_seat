package bootstrap

import (
    "context"
    "os/signal"
    "syscall"
    "time"
)

// StartupTimeout caps how long a process waits for its dependencies.
//
// Past it, a wrong address is a startup failure with a clear message rather than
// a process that hangs and looks alive to whatever is watching it.
const StartupTimeout = 30 * time.Second

// StartupContext is the deadline every dependency has to open inside.
//
// Return:
//   - the context, and the cancel the caller defers
func StartupContext() (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), StartupTimeout)
}

// ShutdownSignal is a context that ends when the container runtime asks this
// process to stop.
//
// Both signals are honoured. SIGTERM is what a container runtime sends, and
// SIGINT is what a person pressing ctrl-c sends, and a process that only handled
// one of them would either ignore a deployment or ignore a developer.
//
// Return:
//   - the context, and the stop the caller defers
func ShutdownSignal() (context.Context, context.CancelFunc) {
    return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}
