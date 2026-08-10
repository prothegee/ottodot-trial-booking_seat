// Package worker is where the queue meets the domain.
//
// The queue package knows nothing about bookings, and the booking and payment
// packages know nothing about jobs. Neither of those is an accident: a queue
// that understood seats would be untestable without a database, and a booking
// package that understood jobs would drag the queue into every request path.
//
// This package is the seam between them. It claims work, hands each job to the
// one thing that understands its kind, and decides what happens when that thing
// fails. Everything domain-shaped is behind a small interface declared here, so
// the runner can be tested without a booking or a charge existing at all.
package worker

import (
	"context"

	"ottodot-trial-booking/backend/internal/queue"
)

// Handler runs one job.
//
// The return value carries more meaning than it looks like it does, so it is
// worth being exact:
//
//	nil     the job is finished and is removed. That includes the case where
//	        there turned out to be nothing to do, because a job that arrives
//	        after a parent already paid has succeeded at its purpose
//	error   the job is handed back and tried again after a backoff, until its
//	        attempts run out and it parks
//
// A handler that cannot tell those two apart will either lose work or retry
// forever, which is why every handler in this package decides it explicitly
// rather than passing an error along.
type Handler interface {
	Handle(ctx context.Context, job queue.Job) error
}

// HandlerFunc lets a plain function be a handler, which is what a test uses to
// stand in for one.
type HandlerFunc func(ctx context.Context, job queue.Job) error

// Handle runs the function.
func (handle HandlerFunc) Handle(ctx context.Context, job queue.Job) error {
	return handle(ctx, job)
}

// Registry maps each kind to the one thing that runs it.
//
// It is a map rather than a switch so a job kind with nothing registered is a
// value the runner can report, instead of a case that silently falls through.
type Registry map[queue.Kind]Handler

// Register adds a handler for one kind.
//
// Param:
// kind - queue.Kind (which jobs this handler runs)
// handler - Handler (the thing that runs them)
//
// Return:
//   - nil once the handler is registered
//   - ErrUnknownKind for a kind this service does not run
//   - ErrHandlerMissing when the handler itself is nil, refused here rather
//     than as a panic on the first job
//   - ErrHandlerAlreadyRegistered when the kind already has one, because a
//     second registration would silently replace the first
func (registry Registry) Register(kind queue.Kind, handler Handler) error {
	if !kind.IsKnown() {
		return ErrUnknownKind
	}

	if handler == nil {
		return ErrHandlerMissing
	}

	if _, taken := registry[kind]; taken {
		return ErrHandlerAlreadyRegistered
	}

	registry[kind] = handler

	return nil
}

// Covers reports whether every kind this service runs has a handler.
//
// The runner checks it at construction. A missing handler discovered at three
// in the morning is a queue filling up, and discovered at startup it is a line
// in a log before anything is accepted.
func (registry Registry) Covers() bool {
	for _, kind := range queue.AllKinds() {
		if _, found := registry[kind]; !found {
			return false
		}
	}

	return true
}
