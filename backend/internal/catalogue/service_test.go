package catalogue_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/catalogue"
)

// catalogueMoment is when the seeded classes start.
var catalogueMoment = time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC)

const (
    classScience = "11111111-1111-7111-8111-111111111111"
    classMath    = "22222222-2222-7222-8222-222222222222"
)

// seededReader puts two classes in front of the service, one starting after the
// other, so ordering can be asserted.
func seededReader() *catalogue.MemoryReader {
    reader := catalogue.NewMemoryReader()

    reader.AddClass(catalogue.Class{
        ID: classMath, Subject: "math", Title: "Math trial",
        StartsAt: catalogueMoment.Add(time.Hour), DurationMinutes: 60,
        Capacity: 4, SeatsRemaining: 4,
    })

    reader.AddClass(catalogue.Class{
        ID: classScience, Subject: "science", Title: "Science trial",
        StartsAt: catalogueMoment, DurationMinutes: 60,
        Capacity: 4, SeatsRemaining: 2,
    })

    return reader
}

func TestReadingTheCatalogue(t *testing.T) {
    t.Run("integration: every class comes back, soonest first", func(t *testing.T) {
        service, err := catalogue.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        listed, err := service.Classes(context.Background())
        if err != nil {
            t.Fatalf("cannot list: %v", err)
        }

        if len(listed) != 2 {
            t.Fatalf("%d classes were listed, wanted 2", len(listed))
        }

        if listed[0].ID != classScience {
            t.Fatalf("the list opens with %s, wanted the class that starts first", listed[0].ID)
        }
    })

    t.Run("unit: one class comes back with its advisory seat count", func(t *testing.T) {
        service, err := catalogue.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        class, err := service.Class(context.Background(), classScience)
        if err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if class.SeatsRemaining != 2 {
            t.Fatalf("the class reports %d seats remaining, wanted 2", class.SeatsRemaining)
        }
    })

    t.Run("unit: a class with nothing left reads as full", func(t *testing.T) {
        reader := seededReader()
        reader.AddClass(catalogue.Class{
            ID: classScience, Subject: "science", Title: "Science trial",
            StartsAt: catalogueMoment, Capacity: 4, SeatsRemaining: 0,
        })

        service, err := catalogue.NewService(reader)
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        class, err := service.Class(context.Background(), classScience)
        if err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if !class.IsFull() {
            t.Fatal("a class with no seats left did not read as full")
        }
    })

    t.Run("edge: an empty catalogue answers with a list rather than nothing", func(t *testing.T) {
        service, err := catalogue.NewService(catalogue.NewMemoryReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        listed, err := service.Classes(context.Background())
        if err != nil {
            t.Fatalf("cannot list: %v", err)
        }

        if listed == nil {
            t.Fatal("an empty catalogue answered nil, so a client would have to guard against null")
        }

        if len(listed) != 0 {
            t.Fatalf("an empty catalogue listed %d classes", len(listed))
        }
    })

    t.Run("edge: a class nobody has answers not found", func(t *testing.T) {
        service, err := catalogue.NewService(seededReader())
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        _, err = service.Class(context.Background(), "33333333-3333-7333-8333-333333333333")

        if !errors.Is(err, catalogue.ErrClassNotFound) {
            t.Fatalf("an unknown class answered %v", err)
        }
    })

    t.Run("edge: a read naming nothing is refused before storage is touched", func(t *testing.T) {
        reader := seededReader()

        service, err := catalogue.NewService(reader)
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        before := reader.Reads()

        if _, err := service.Class(context.Background(), "   "); !errors.Is(err, catalogue.ErrInvalidRequest) {
            t.Fatalf("a read naming nothing answered %v", err)
        }

        if reader.Reads() != before {
            t.Fatal("a refused read still reached storage")
        }
    })

    t.Run("edge: a service with no reader is refused at construction", func(t *testing.T) {
        if _, err := catalogue.NewService(nil); err == nil {
            t.Fatal("a service with nothing to read from was built")
        }
    })
}

func TestTheReaderCountsWhatItWasAsked(t *testing.T) {
    t.Run("unit: every read is counted, so an untouched database can be proven untouched", func(t *testing.T) {
        reader := seededReader()

        if reader.Reads() != 0 {
            t.Fatalf("a fresh reader reports %d reads", reader.Reads())
        }

        service, err := catalogue.NewService(reader)
        if err != nil {
            t.Fatalf("cannot build the service: %v", err)
        }

        if _, err := service.Classes(context.Background()); err != nil {
            t.Fatalf("cannot list: %v", err)
        }

        if reader.Reads() != 1 {
            t.Fatalf("one list was counted as %d reads", reader.Reads())
        }
    })
}
