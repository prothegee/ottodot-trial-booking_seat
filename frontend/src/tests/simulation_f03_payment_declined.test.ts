import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { idempotencyKeyHeader } from "$lib/api/idempotency";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import { remainingFor } from "$lib/booking/countdown";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import { bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";

/**
 * Simulation F3: the payment was declined.
 *
 *     parent -> client: submit payment
 *     client -> api: pay
 *     api -> client: 402 payment_declined
 *     client -> parent: declined, retry on the same booking
 *     note over client: hold countdown keeps running, booking still pending_payment
 *
 * Asserts: the booking is not abandoned, the countdown continues, and the retry
 * reuses the booking while issuing a fresh idempotency key for the new attempt.
 *
 * The key rule is the part worth reading. A decline is a finished attempt: the
 * provider looked at it and said no, and no money moved. Sending the same key
 * back would replay the decline for as long as the parent kept trying.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

/** The instant this runs at, so the countdown is not a moving target. */
const now = Date.parse("2026-08-11T09:00:00.000Z");

/** The hold the api granted, ten minutes from that instant. */
const holdDeadline = new Date(now + 10 * 60 * 1000).toISOString();

const heldBooking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: holdDeadline,
};

const confirmedBooking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** The client, with the provider declining the first payment and taking the second. */
function newStage() {
    let payments = 0;

    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: { classes: [] }, headers: { etag: '"v1"' } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            return { status: 201, body: heldBooking };
        }

        if (request.method === "POST" && request.path === paymentPathFor(bookingId)) {
            payments += 1;

            if (payments === 1) {
                return { status: 402, body: errorBody("payment_declined") };
            }

            return { status: 200, body: confirmedBooking };
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

describe("simulation F3: the payment was declined", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the parent is told, in this client's words, that no money was taken", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });

        const refused = await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(refused).toBeNull();
        expect(get(stage.booking).failure?.kind).toBe("PaymentDeclined");
        expect(get(stage.booking).failure?.message).toBe("the payment was declined, no money was taken");
    });

    test("behaviour: the booking is not abandoned, so the hold is not thrown away", async () => {
        // Clearing it would send the parent back to the class list to start
        // over while their seat is still held for them.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const held = get(stage.booking).booking;

        expect(held?.id).toBe(bookingId);
        expect(held?.status).toBe("pending_payment");
    });

    test("behaviour: the countdown keeps running, because the hold did not move", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const left = remainingFor(get(stage.booking).booking?.hold_expires_at ?? null, now + 60 * 1000);

        expect(left.expired).toBe(false);
        expect(left.label).toBe("09:00");
    });

    test("behaviour: the retry reuses the booking and earns a fresh key", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const settled = await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(settled?.status).toBe("confirmed");
        expect(settled?.seat_no).toBe(2);

        const payments = stage.transport.callsTo("POST", paymentPathFor(bookingId));

        expect(payments).toHaveLength(2);

        const first = payments[0].headers?.[idempotencyKeyHeader];
        const second = payments[1].headers?.[idempotencyKeyHeader];

        expect(first).toBeTruthy();
        expect(second).toBeTruthy();
        expect(second).not.toBe(first);
    });

    test("edge: a decline says nothing about seat counts, so the cached list is left alone", async () => {
        // No seat moved. Ageing the entry would cost a request to learn a
        // number that has not changed.
        const stage = newStage();

        // The list is read after the hold is granted, because granting one
        // does move a seat count and does age the entry. What this case is
        // about is the decline that follows, which moves nothing.
        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });
        await stage.classes.load();

        expect(get(stage.classes).lastResult).toBe("fresh");
        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(1);
    });
});
