package booking_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"ottodot-trial-booking/backend/internal/booking"
)

// everyFailure is the full set this package can report. Any new one belongs
// here, which is what keeps the checks below covering all of them.
var everyFailure = map[string]error{
	"ErrInvalidRequest":    booking.ErrInvalidRequest,
	"ErrClassNotFound":     booking.ErrClassNotFound,
	"ErrStudentNotFound":   booking.ErrStudentNotFound,
	"ErrBookingNotFound":   booking.ErrBookingNotFound,
	"ErrAlreadyBooked":     booking.ErrAlreadyBooked,
	"ErrTooManyHolds":      booking.ErrTooManyHolds,
	"ErrClassFull":         booking.ErrClassFull,
	"ErrSeatLost":          booking.ErrSeatLost,
	"ErrNotHolding":        booking.ErrNotHolding,
	"ErrInvalidTransition": booking.ErrInvalidTransition,
}

// identifierShape matches anything that looks like a uuid, which is the form
// every identifier in this service takes.
var identifierShape = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}`)

func TestEveryFailureIsDistinct(t *testing.T) {
	t.Run("unit: no two failures compare equal", func(t *testing.T) {
		// Sentinels are compared with errors.Is, so two that matched each other
		// would send a parent the wrong reason with no test failing anywhere
		// else.
		for firstName, first := range everyFailure {
			for secondName, second := range everyFailure {
				if firstName == secondName {
					continue
				}

				if errors.Is(first, second) {
					t.Errorf("%s and %s are not distinguishable", firstName, secondName)
				}
			}
		}
	})
}

func TestNoFailureMessageLeaksSomethingItShouldNot(t *testing.T) {
	t.Run("edge: no message carries an identifier", func(t *testing.T) {
		// These strings reach a log, and a log gets pasted into a chat window.
		for name, failure := range everyFailure {
			if identifierShape.MatchString(failure.Error()) {
				t.Errorf("%s carries something that looks like an identifier: %q", name, failure.Error())
			}
		}
	})

	t.Run("edge: every message names the package it came from", func(t *testing.T) {
		for name, failure := range everyFailure {
			if !strings.HasPrefix(failure.Error(), "booking: ") {
				t.Errorf("%s does not say where it came from: %q", name, failure.Error())
			}
		}
	})

	t.Run("edge: no message is empty", func(t *testing.T) {
		for name, failure := range everyFailure {
			if strings.TrimSpace(strings.TrimPrefix(failure.Error(), "booking:")) == "" {
				t.Errorf("%s has nothing to say", name)
			}
		}
	})
}
