package booking

import (
	"context"
	"sort"
	"sync"
	"time"

	"ottodot-trial-booking/backend/internal/identifier"
)

// MemoryRepository is the fake every fast test runs against.
//
// It holds the same invariants the database holds, under one mutex instead of a
// row lock. That is the point of it: the four fast tiers can prove the service
// calls the right things in the right order, in a second, with nothing running.
//
// What it cannot prove is that SELECT ... FOR UPDATE serializes two real
// transactions, because there is no transaction here. That is why the same
// behaviour suite also runs against Postgres in the proof tier.
type MemoryRepository struct {
	mutex    sync.Mutex
	classes  map[string]Class
	parents  map[string]string
	bookings map[string]Booking
	events   map[string][]Event
}

// NewMemoryRepository builds an empty repository. Classes and students are put
// in with AddClass and AddStudent, because a fake with rows already in it hides
// what a test depends on.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		classes:  make(map[string]Class),
		parents:  make(map[string]string),
		bookings: make(map[string]Booking),
		events:   make(map[string][]Event),
	}
}

// AddClass puts a class in front of the repository. It stands in for a row in
// trial_classes.
func (repository *MemoryRepository) AddClass(class Class) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	repository.classes[class.ID] = class
}

// AddStudent records which parent a child belongs to. It stands in for a row in
// students, and it is the only reason this repository knows about parents at
// all: the hold cap is counted per parent.
func (repository *MemoryRepository) AddStudent(studentID string, parentID string) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	repository.parents[studentID] = parentID
}

// Class reads one class.
func (repository *MemoryRepository) Class(_ context.Context, classID string) (Class, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	class, found := repository.classes[classID]
	if !found {
		return Class{}, ErrClassNotFound
	}

	return class, nil
}

// Booking reads one booking.
func (repository *MemoryRepository) Booking(_ context.Context, bookingID string) (Booking, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	stored, found := repository.bookings[bookingID]
	if !found {
		return Booking{}, ErrBookingNotFound
	}

	return stored, nil
}

// SeatsTaken lists the seats currently held in a class, ascending.
func (repository *MemoryRepository) SeatsTaken(_ context.Context, classID string) ([]int16, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	return repository.seatsTakenLocked(classID), nil
}

// Events reads the audit trail for one booking, oldest first.
func (repository *MemoryRepository) Events(_ context.Context, bookingID string) ([]Event, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	trail := make([]Event, len(repository.events[bookingID]))
	copy(trail, repository.events[bookingID])

	return trail, nil
}

// Hold grants a place on the payment screen.
//
// The checks run in the order a parent would want to hear them: the most
// specific answer first. Already booked says exactly what happened, the hold
// cap is about this parent, and a full class is about everyone.
func (repository *MemoryRepository) Hold(_ context.Context, request HoldRequest) (Booking, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	class, found := repository.classes[request.ClassID]
	if !found {
		return Booking{}, ErrClassNotFound
	}

	parentID, found := repository.parents[request.StudentID]
	if !found {
		return Booking{}, ErrStudentNotFound
	}

	if repository.hasLiveBookingLocked(request.StudentID, request.ClassID) {
		return Booking{}, ErrAlreadyBooked
	}

	if repository.liveHoldsForParentLocked(parentID, request.Now) >= request.MaxHoldsPerParent {
		return Booking{}, ErrTooManyHolds
	}

	if repository.holdersLocked(request.ClassID, request.Now) >= MaxHolders(class) {
		return Booking{}, ErrClassFull
	}

	granted := Booking{
		ID:            request.BookingID,
		StudentID:     request.StudentID,
		ClassID:       request.ClassID,
		Status:        StatusPendingPayment,
		HoldExpiresAt: request.ExpiresAt,
		CreatedAt:     request.Now,
		UpdatedAt:     request.Now,
	}

	repository.bookings[granted.ID] = granted

	if err := repository.recordLocked(granted.ID, "", StatusPendingPayment, ActorParent, "hold granted", request.Now); err != nil {
		return Booking{}, err
	}

	return granted, nil
}

// Confirm runs the last-seat decision.
//
// The hold deadline is deliberately not consulted here. By the time a booking
// reaches this point the money has already moved, and if a seat is still free
// then handing it over is better for everyone than refunding a parent because a
// countdown ran out a moment earlier. A hold the worker already expired is
// caught by the status check instead, which is the honest signal.
func (repository *MemoryRepository) Confirm(_ context.Context, request ConfirmRequest) (Booking, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	stored, found := repository.bookings[request.BookingID]
	if !found {
		return Booking{}, ErrBookingNotFound
	}

	if stored.Status != StatusPendingPayment {
		return stored, ErrNotHolding
	}

	class, found := repository.classes[stored.ClassID]
	if !found {
		return Booking{}, ErrClassNotFound
	}

	seat, free := LowestFreeSeat(class.Capacity, repository.seatsTakenLocked(stored.ClassID))
	if !free {
		lost := stored
		lost.Status = StatusRefundRequired
		lost.HoldExpiresAt = time.Time{}
		lost.UpdatedAt = request.Now

		repository.bookings[lost.ID] = lost

		if err := repository.recordLocked(lost.ID, stored.Status, StatusRefundRequired, ActorSystem, "no free seat under the class lock", request.Now); err != nil {
			return Booking{}, err
		}

		return lost, ErrSeatLost
	}

	won := stored
	won.Status = StatusConfirmed
	won.SeatNo = seat
	won.ConfirmedAt = request.Now
	won.HoldExpiresAt = time.Time{}
	won.UpdatedAt = request.Now

	repository.bookings[won.ID] = won

	if err := repository.recordLocked(won.ID, stored.Status, StatusConfirmed, ActorSystem, "seat assigned under the class lock", request.Now); err != nil {
		return Booking{}, err
	}

	return won, nil
}

// Cancel withdraws a booking and releases the seat it held, which is what makes
// the seat available to the next confirm.
func (repository *MemoryRepository) Cancel(_ context.Context, request CancelRequest) (Booking, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	stored, found := repository.bookings[request.BookingID]
	if !found {
		return Booking{}, ErrBookingNotFound
	}

	if !CanTransition(stored.Status, StatusCancelled) {
		return stored, ErrInvalidTransition
	}

	withdrawn := stored
	withdrawn.Status = StatusCancelled
	withdrawn.SeatNo = 0
	withdrawn.HoldExpiresAt = time.Time{}
	withdrawn.UpdatedAt = request.Now

	repository.bookings[withdrawn.ID] = withdrawn

	if err := repository.recordLocked(withdrawn.ID, stored.Status, StatusCancelled, request.Actor, request.Reason, request.Now); err != nil {
		return Booking{}, err
	}

	return withdrawn, nil
}

// hasLiveBookingLocked mirrors the uq_booking_active index: one live booking per
// child per class, whether or not its hold has lapsed. A lapsed hold is
// released by the worker, and until it is, the index would refuse a second row
// anyway.
func (repository *MemoryRepository) hasLiveBookingLocked(studentID string, classID string) bool {
	for _, stored := range repository.bookings {
		if stored.StudentID != studentID || stored.ClassID != classID {
			continue
		}

		if stored.Status.IsLive() {
			return true
		}
	}

	return false
}

// liveHoldsForParentLocked counts holds still standing for one parent, across
// every class.
func (repository *MemoryRepository) liveHoldsForParentLocked(parentID string, now time.Time) int {
	standing := 0

	for _, stored := range repository.bookings {
		if repository.parents[stored.StudentID] != parentID {
			continue
		}

		if stored.Status != StatusPendingPayment {
			continue
		}

		if HoldIsLive(stored.HoldExpiresAt, now) {
			standing++
		}
	}

	return standing
}

// holdersLocked counts how many parents are on the payment screen for one
// class, plus the seats already confirmed.
//
// A lapsed hold is not counted, which is the one place this differs from the
// duplicate rule above. The reason is what each rule is for: the duplicate rule
// mirrors a database invariant, this one describes who is actually sitting on
// the payment screen right now.
func (repository *MemoryRepository) holdersLocked(classID string, now time.Time) int {
	holders := 0

	for _, stored := range repository.bookings {
		if stored.ClassID != classID {
			continue
		}

		switch stored.Status {
		case StatusConfirmed:
			holders++
		case StatusPendingPayment:
			if HoldIsLive(stored.HoldExpiresAt, now) {
				holders++
			}
		}
	}

	return holders
}

// seatsTakenLocked lists held seats ascending, so the caller gets the same order
// the sql version produces.
func (repository *MemoryRepository) seatsTakenLocked(classID string) []int16 {
	var seats []int16

	for _, stored := range repository.bookings {
		if stored.ClassID != classID || !stored.HasSeat() {
			continue
		}

		seats = append(seats, stored.SeatNo)
	}

	sort.Slice(seats, func(first int, second int) bool { return seats[first] < seats[second] })

	return seats
}

// recordLocked appends one line to the audit trail. The id is minted here
// because an event is this repository's own record of what it did.
func (repository *MemoryRepository) recordLocked(bookingID string, from Status, to Status, actor Actor, reason string, now time.Time) error {
	eventID, err := identifier.NewUUIDv7()
	if err != nil {
		return err
	}

	repository.events[bookingID] = append(repository.events[bookingID], Event{
		ID:         eventID,
		BookingID:  bookingID,
		FromStatus: from,
		ToStatus:   to,
		Actor:      actor,
		Reason:     reason,
		CreatedAt:  now,
	})

	return nil
}
