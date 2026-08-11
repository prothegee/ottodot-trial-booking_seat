package roster_test

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/roster"
)

const (
    classScience = "11111111-1111-7111-8111-111111111111"
    classEmpty   = "22222222-2222-7222-8222-222222222222"
)

// rosterMoment is when the seeded bookings confirmed.
var rosterMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// seededReader seats two children out of order, so the ordering assertion means
// something.
func seededReader() *roster.MemoryReader {
    reader := roster.NewMemoryReader()

    reader.AddClass(classScience, 4)
    reader.AddClass(classEmpty, 4)

    reader.AddEntry(classScience, roster.Entry{
        SeatNo: 3, StudentID: "student-c", StudentName: "Citra Halim", ConfirmedAt: rosterMoment,
    })

    reader.AddEntry(classScience, roster.Entry{
        SeatNo: 1, StudentID: "student-a", StudentName: "Adi Halim", ConfirmedAt: rosterMoment,
    })

    return reader
}

func TestReadingARoster(t *testing.T) {
    t.Run("integration: everyone with a seat comes back, in seat order", func(t *testing.T) {
        service, err := roster.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        seated, err := service.For(context.Background(), classScience)
        if err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if seated.SeatsTaken() != 2 {
            t.Fatalf("%d seats were reported taken, wanted 2", seated.SeatsTaken())
        }

        if seated.Entries[0].SeatNo != 1 || seated.Entries[1].SeatNo != 3 {
            t.Fatalf("the roster is not in seat order: %d then %d",
                seated.Entries[0].SeatNo, seated.Entries[1].SeatNo)
        }
    })

    t.Run("unit: the capacity comes back with the roster, so an empty seat is visible", func(t *testing.T) {
        service, err := roster.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        seated, err := service.For(context.Background(), classScience)
        if err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if seated.Capacity != 4 {
            t.Fatalf("the roster reports a capacity of %d", seated.Capacity)
        }
    })

    t.Run("edge: a class nobody has booked is an empty roster, not a missing one", func(t *testing.T) {
        service, err := roster.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        seated, err := service.For(context.Background(), classEmpty)
        if err != nil {
            t.Fatalf("an empty class answered %v, and an empty class is a real answer", err)
        }

        if seated.Entries == nil {
            t.Fatal("an empty roster answered nil, so a client would have to guard against null")
        }

        if seated.SeatsTaken() != 0 {
            t.Fatalf("an empty class reports %d seats taken", seated.SeatsTaken())
        }
    })

    t.Run("edge: a class that does not exist answers not found", func(t *testing.T) {
        service, err := roster.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        _, err = service.For(context.Background(), "33333333-3333-7333-8333-333333333333")

        if !errors.Is(err, roster.ErrClassNotFound) {
            t.Fatalf("an unknown class answered %v", err)
        }
    })

    t.Run("edge: a read naming nothing is refused", func(t *testing.T) {
        service, err := roster.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        if _, err := service.For(context.Background(), "  "); !errors.Is(err, roster.ErrInvalidRequest) {
            t.Fatalf("a read naming nothing answered %v", err)
        }
    })

    t.Run("edge: a service with no reader is refused at construction", func(t *testing.T) {
        if _, err := roster.NewService(nil); err == nil {
            t.Fatal("a service with nothing to read from was built")
        }
    })

    t.Run("edge: no failure in this package ever names a child", func(t *testing.T) {
        for _, failure := range []error{roster.ErrInvalidRequest, roster.ErrClassNotFound} {
            for _, name := range []string{"Adi", "Citra", "Halim", "student-a"} {
                if strings.Contains(failure.Error(), name) {
                    t.Fatalf("the failure %q carries %q, and a failure string reaches a log", failure, name)
                }
            }
        }
    })
}
