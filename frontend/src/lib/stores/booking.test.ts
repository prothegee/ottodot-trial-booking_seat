import { describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import { idempotencyKeyHeader } from "$lib/api/idempotency";
import type { TransportRequest } from "$lib/api/transport";
import type { Booking } from "$lib/api/types";
import { bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";

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

const confirmedBooking: Booking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** A mutator that answers from a script and records every request. */
function stubMutator(answers: (unknown | Error)[]) {
    const sent: TransportRequest[] = [];
    let call = 0;

    return {
        sent,

        send<T>(request: TransportRequest): Promise<T> {
            sent.push(request);

            const answer = answers[Math.min(call, answers.length - 1)];
            call += 1;

            if (answer instanceof Error) {
                return Promise.reject(answer);
            }

            return Promise.resolve(answer as T);
        },
    };
}

/** A key source a test can read back, instead of matching a uuid. */
function countingKeys() {
    let minted = 0;

    return () => {
        minted += 1;

        return `key-${minted}`;
    };
}

describe("the booking store", () => {
    test("integration: creating a booking sends the child, the class, and a key", async () => {
        const mutator = stubMutator([heldBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        const held = await booking.create({ student_id: studentId, class_id: classId });

        expect(held?.id).toBe(bookingId);
        expect(mutator.sent).toHaveLength(1);
        expect(mutator.sent[0].method).toBe("POST");
        expect(mutator.sent[0].path).toBe(bookingsPath);
        expect(mutator.sent[0].body).toEqual({ student_id: studentId, class_id: classId });
        expect(mutator.sent[0].headers?.[idempotencyKeyHeader]).toBe("key-1");
    });

    test("integration: the booking and its deadline land in the store", async () => {
        const mutator = stubMutator([heldBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });

        const state = get(booking);

        expect(state.booking?.status).toBe("pending_payment");
        expect(state.booking?.hold_expires_at).toBe("2026-08-11T09:10:00Z");
        expect(state.submitting).toBe(false);
        expect(state.failure).toBeNull();
    });

    test("unit: paying reuses the key the attempt already has", async () => {
        // One key covers one attempt. A second key on the payment would let a
        // retry of it charge twice.
        const mutator = stubMutator([heldBooking, confirmedBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });
        await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(mutator.sent).toHaveLength(2);
        expect(mutator.sent[1].path).toBe(paymentPathFor(bookingId));
        expect(mutator.sent[1].headers?.[idempotencyKeyHeader]).toBe("key-1");
    });

    test("integration: a settled payment leaves the confirmed booking and its seat", async () => {
        const mutator = stubMutator([heldBooking, confirmedBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });
        const paid = await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(paid?.status).toBe("confirmed");
        expect(get(booking).booking?.seat_no).toBe(2);
    });

    test("edge: a new attempt after a decline gets a fresh key", async () => {
        // A decline is a finished attempt. Reusing the key would replay the
        // decline for as long as the parent kept trying.
        const mutator = stubMutator([
            heldBooking,
            new ApiError("PaymentDeclined", "payment_declined", 402),
            confirmedBooking,
        ]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });
        await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        booking.startNewAttempt();

        await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(mutator.sent[1].headers?.[idempotencyKeyHeader]).toBe("key-1");
        expect(mutator.sent[2].headers?.[idempotencyKeyHeader]).toBe("key-2");
    });

    test("edge: a decline keeps the booking, because the hold is still standing", async () => {
        // Throwing it away would send the parent back to the class list to
        // start over while their seat is still held for them.
        const mutator = stubMutator([heldBooking, new ApiError("PaymentDeclined", "payment_declined", 402)]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });
        const refused = await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(refused).toBeNull();
        expect(get(booking).booking?.id).toBe(bookingId);
        expect(get(booking).failure?.kind).toBe("PaymentDeclined");
    });

    test("edge: a duplicate carries the booking the child already has", async () => {
        // Without it the parent is told a booking exists and left to find it.
        const existing = "0192a000-0000-7000-8000-000000000099";
        const mutator = stubMutator([new ApiError("AlreadyBooked", "already_booked", 409, 0, "", existing)]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });

        expect(get(booking).failure?.kind).toBe("AlreadyBooked");
        expect(get(booking).failure?.bookingId).toBe(existing);
    });

    test("edge: a failure that is not from the api still leaves a sentence", async () => {
        const mutator = stubMutator([new Error("the network is on fire")]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });

        expect(get(booking).failure?.message).toBe("something went wrong, your booking was not changed");
        expect(get(booking).submitting).toBe(false);
    });

    test("edge: paying with no attempt in progress still sends a key", async () => {
        // An empty header would be refused by the api, which is a worse answer
        // than a key nobody asked for.
        const mutator = stubMutator([confirmedBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(mutator.sent[0].headers?.[idempotencyKeyHeader]).toBe("key-1");
    });

    test("edge: resetting empties everything, including the key", async () => {
        const mutator = stubMutator([heldBooking, heldBooking]);
        const booking = createBookingStore({ mutator, newKey: countingKeys() });

        await booking.create({ student_id: studentId, class_id: classId });
        booking.reset();

        expect(get(booking)).toEqual({ booking: null, attemptKey: "", submitting: false, failure: null });

        // The next attempt mints its own key rather than picking the old one
        // back up, which a reset that only cleared the state would allow.
        await booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(mutator.sent[1].headers?.[idempotencyKeyHeader]).toBe("key-2");
    });
});
