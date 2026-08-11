package queue_test

import (
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/queue"
)

const payloadBooking = "0192e000-0000-7000-8000-000000000001"

func TestAPayloadSurvivesTheRoundTrip(t *testing.T) {
    t.Run("unit: what goes in comes back out", func(t *testing.T) {
        encoded, err := queue.EncodeBookingPayload(payloadBooking)
        if err != nil {
            t.Fatalf("expected the payload to encode, got: %v", err)
        }

        decoded, err := queue.DecodeBookingPayload(encoded)
        if err != nil {
            t.Fatalf("expected the payload to decode, got: %v", err)
        }

        if decoded.BookingID != payloadBooking {
            t.Fatalf("expected %s, got %s", payloadBooking, decoded.BookingID)
        }
    })

    t.Run("unit: the encoded form is the field name the column stores", func(t *testing.T) {
        // The name is asserted rather than assumed, because a worker running an
        // older build reads payloads a newer one wrote. Renaming this field
        // silently would strand every job already in the table.
        encoded, err := queue.EncodeBookingPayload(payloadBooking)
        if err != nil {
            t.Fatalf("expected the payload to encode, got: %v", err)
        }

        expected := `{"booking_id":"` + payloadBooking + `"}`

        if string(encoded) != expected {
            t.Fatalf("expected %s, got %s", expected, encoded)
        }
    })
}

func TestAPayloadNobodyCanActOnIsRefused(t *testing.T) {
    t.Run("edge: encoding an empty booking is refused", func(t *testing.T) {
        _, err := queue.EncodeBookingPayload("")
        if !errors.Is(err, queue.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }
    })

    t.Run("edge: bytes that are not json are refused", func(t *testing.T) {
        _, err := queue.DecodeBookingPayload([]byte("not json"))
        if !errors.Is(err, queue.ErrInvalidPayload) {
            t.Fatalf("expected ErrInvalidPayload, got: %v", err)
        }
    })

    t.Run("edge: a well formed object with no booking is refused", func(t *testing.T) {
        // This is the case a schema check alone would let through: valid json,
        // valid object, and useless to the handler that receives it.
        _, err := queue.DecodeBookingPayload([]byte(`{"class_id":"x"}`))
        if !errors.Is(err, queue.ErrInvalidPayload) {
            t.Fatalf("expected ErrInvalidPayload, got: %v", err)
        }
    })

    t.Run("edge: an empty payload is refused", func(t *testing.T) {
        _, err := queue.DecodeBookingPayload(nil)
        if !errors.Is(err, queue.ErrInvalidPayload) {
            t.Fatalf("expected ErrInvalidPayload, got: %v", err)
        }
    })
}
