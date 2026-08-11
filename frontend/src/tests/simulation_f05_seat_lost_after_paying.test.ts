import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import type { Booking } from "$lib/api/types";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import BookingStatus from "$lib/components/BookingStatus.svelte";
import { bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";
import { trialPayment } from "$lib/booking/price";

/**
 * Simulation F5: the seat was lost after paying, the brief's scenario from the
 * parent's side.
 *
 *     parent A -> client: submit payment for the last seat
 *     client -> api: pay
 *     api -> client: 409 seat_lost, refund queued
 *     client -> parent A: seat was taken, payment is being refunded, nothing to do
 *     note over client: no retry control offered, this is terminal
 *
 * Asserts: `SeatLost` renders a terminal message with no retry control, and it
 * is visually and textually distinct from `PaymentDeclined`, because one took
 * money and one did not.
 *
 * That distinction is the whole point of the case. Both are refusals of the
 * same call, and telling them apart is the difference between a parent who
 * waits for a refund and a parent who tries to pay a second time.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

const heldBooking: Booking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: "2026-08-11T09:10:00Z",
};

/** What the api reports the booking as once the refund is queued. */
const owedBooking: Booking = { ...heldBooking, status: "refund_required", hold_expires_at: null };

/** The same booking after a decline, which took no money at all. */
const declinedBooking: Booking = { ...heldBooking, status: "payment_failed", hold_expires_at: null };

/** One class, with the last seat still showing when the parent submits. */
const classList = {
    classes: [
        {
            id: classId,
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 1,
        },
    ],
};

/** The client, with somebody else winning the seat between the hold and the payment. */
function newStage() {
    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: classList, headers: { etag: '"v1"' } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            return { status: 201, body: heldBooking };
        }

        if (request.method === "POST" && request.path === paymentPathFor(bookingId)) {
            // The money moved and then the seat did not. This is the case the
            // whole design exists for.
            return { status: 409, body: errorBody("seat_lost") };
        }

        return { status: 404, body: errorBody("invalid_request") };
    });

    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore();

    return {
        transport,
        classes: createClassesStore({ reader: createCachedReader({ client, store }) }),
        booking: createBookingStore({ mutator: createCacheAwareMutator({ client, store }), cache: store }),
    };
}

describe("simulation F5: the seat went to somebody else after the payment settled", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the parent is told the seat is gone and a refund is on the way", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });

        const refused = await stage.booking.pay(bookingId, trialPayment());

        expect(refused).toBeNull();
        expect(get(stage.booking).failure?.kind).toBe("SeatLost");
        expect(get(stage.booking).failure?.message).toBe(
            "the last seat went to someone else, a refund is on the way",
        );
    });

    test("behaviour: this is terminal, and no second payment is attempted", async () => {
        // A retry here would be a second charge on a booking that has no seat.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, trialPayment());

        expect(stage.transport.callsTo("POST", paymentPathFor(bookingId))).toHaveLength(1);
    });

    test("behaviour: it reads differently from a decline, because one took money and one did not", () => {
        const lost = render(BookingStatus, { props: { booking: owedBooking } });

        expect(screen.getByTestId("booking-status-guidance")).toHaveTextContent(/refunded/i);
        expect(screen.getByTestId("booking-status-guidance")).toHaveTextContent(/nothing for you to do/i);

        lost.unmount();

        render(BookingStatus, { props: { booking: declinedBooking } });

        expect(screen.getByTestId("booking-status-guidance")).toHaveTextContent(/no money was taken/i);
    });

    test("behaviour: the status element carries the state, so a screen never matches on prose", () => {
        render(BookingStatus, { props: { booking: owedBooking } });

        expect(screen.getByTestId("booking-status")).toHaveAttribute("data-status", "refund_required");
    });

    test("behaviour: a lost seat proves the cached count wrong, so the list is read again", async () => {
        // Somebody took the seat this parent was still being shown. Leaving the
        // entry fresh would send them back to a list that still offers it.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();
        await stage.booking.pay(bookingId, trialPayment());
        await stage.classes.load();

        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(2);
    });

    test("edge: the terminal screen offers no payment control at all", async () => {
        // Asserted on the rendered screen rather than on the store, because the
        // promise here is about what a parent can click.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, trialPayment());

        render(BookingStatus, { props: { booking: owedBooking } });

        await waitFor(() => {
            expect(screen.getByTestId("booking-status")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("payment-submit")).not.toBeInTheDocument();
    });
});
