import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { idempotencyKeyHeader } from "$lib/api/idempotency";
import { createFakeTransport } from "$lib/api/transport_fake";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import { bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";

/**
 * Simulation F1: the happy path.
 *
 *     parent -> client: pick a class, pick a child, submit
 *     client -> api: create a booking with an idempotency key
 *     api -> client: pending_payment, and the hold deadline
 *     client -> parent: the payment screen, countdown running
 *     parent -> client: submit the payment
 *     client -> api: pay, with the same key
 *     api -> client: confirmed, seat 2
 *     client -> parent: confirmed, seat number shown
 *
 * Asserts: the deadline the countdown runs from is the one the api returned,
 * the same idempotency key is used for both calls, and the confirmed seat
 * number is what the store ends up holding.
 *
 * The payment screen itself is phase 5. This runs at the store level, which is
 * where both calls and the key actually live, so the screen can be built on top
 * of behaviour that is already proven.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

const holdDeadline = "2026-08-11T09:10:00Z";

const classList = {
    classes: [
        {
            id: classId,
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 3,
        },
    ],
};

const heldBooking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: holdDeadline,
};

const confirmedBooking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** The whole client, wired the way the application wires it. */
function newStage() {
    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: classList, headers: { etag: '"v1"' } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            return { status: 201, body: heldBooking };
        }

        if (request.method === "POST" && request.path === paymentPathFor(bookingId)) {
            return { status: 200, body: confirmedBooking };
        }

        return { status: 404, body: { error: { code: "invalid_request", message: "no such route" } } };
    });

    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore();

    return {
        transport,
        classes: createClassesStore({ reader: createCachedReader({ client, store }) }),
        booking: createBookingStore({ mutator: createCacheAwareMutator({ client, store }) }),
    };
}

describe("simulation F1: a booking from the class list to a confirmed seat", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the parent picks, holds, pays, and ends up with a seat number", async () => {
        const stage = newStage();

        await stage.classes.load();

        expect(get(stage.classes).classes[0].title).toBe("Science Discovery");

        const held = await stage.booking.create({ student_id: studentId, class_id: classId });

        // The countdown runs from what the api said, never from a deadline this
        // client worked out for itself.
        expect(held?.status).toBe("pending_payment");
        expect(held?.hold_expires_at).toBe(holdDeadline);

        const paid = await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(paid?.status).toBe("confirmed");
        expect(paid?.seat_no).toBe(2);
        expect(get(stage.booking).booking?.seat_no).toBe(2);
        expect(get(stage.booking).failure).toBeNull();
    });

    test("behaviour: both calls in the attempt carry the same idempotency key", async () => {
        // A second key on the payment would make a retry of it a new charge,
        // which is the failure that takes money and is noticed on a statement.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const created = stage.transport.callsTo("POST", bookingsPath);
        const settled = stage.transport.callsTo("POST", paymentPathFor(bookingId));

        expect(created).toHaveLength(1);
        expect(settled).toHaveLength(1);

        const key = created[0].headers?.[idempotencyKeyHeader];

        expect(key).toBeTruthy();
        expect(settled[0].headers?.[idempotencyKeyHeader]).toBe(key);
    });

    test("behaviour: the booking invalidates the class list, so the next view is confirmed", async () => {
        // The seat count the parent saw is now out of date by their own doing.
        // Serving it again from memory would put a number on screen that this
        // client already knows is wrong.
        const stage = newStage();

        await stage.classes.load();

        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(1);

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();

        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(2);
    });
});
