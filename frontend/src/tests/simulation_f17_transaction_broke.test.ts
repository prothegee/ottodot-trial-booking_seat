import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { idempotencyKeyHeader } from "$lib/api/idempotency";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import { ApiError } from "$lib/api/errors";
import type { Booking } from "$lib/api/types";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import { api } from "$lib/session/client";
import { classMutator } from "$lib/session/cached_api";
import { booking, bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";
import PayPage from "../routes/pay/[bookingId]/+page.svelte";

/**
 * Simulation F17: the backend transaction breaks mid-payment.
 *
 * The client half of backend simulation 15. The transport answers the payment
 * with 500 `internal_error`, which is what a parent sees when the confirm
 * transaction fails rather than losing a race.
 *
 *     parent -> client: submit payment
 *     client -> api: pay with idempotency key K
 *     api -> client: 500 internal_error with a request id
 *     client -> parent: something broke, your booking is untouched, retry
 *     note over client: no claim about the seat, no claim about the money
 *     parent -> client: retry
 *     client -> api: pay again with the same key K
 *     api -> client: 200 confirmed
 *     client -> parent: confirmed, seat number shown
 *
 * Asserts: the message never says the seat was lost and never says the payment
 * was declined, the booking stays on screen as pending rather than being
 * cleared, the retry reuses the same idempotency key rather than minting a new
 * one, the request id is rendered for quoting, and the cached class list is not
 * aged because nothing is known to have changed.
 *
 * The idempotency detail is the one worth stating. A declined payment is a
 * finished attempt and earns a fresh key, as in simulation F3. An
 * `internal_error` is an attempt of unknown outcome, so the same key must go
 * back, or a retry risks charging twice.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";
const requestId = "0192a000-0000-7000-8000-0000000000f1";

/** The instant this runs at, so the countdown is not a moving target. */
const now = Date.parse("2026-08-11T09:00:00.000Z");

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$app/state", () => ({
    page: { params: { bookingId: "0192a000-0000-7000-8000-000000000031" } },
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

const heldBooking: Booking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: new Date(now + 10 * 60 * 1000).toISOString(),
};

const confirmedBooking: Booking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** The failure the api sends when the transaction broke, carrying its request id. */
const brokenBody = {
    error: {
        code: "internal_error",
        message: "the transaction could not be completed",
        request_id: requestId,
    },
};

/** One class, so the cached list has something in it to leave alone. */
const classList = {
    classes: [
        {
            id: classId,
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 2,
        },
    ],
};

/** The client, with the api breaking on the first payment and settling the second. */
function newStage() {
    let payments = 0;

    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: classList, headers: { etag: '"v1"' } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            return { status: 201, body: heldBooking };
        }

        if (request.method === "POST" && request.path === paymentPathFor(bookingId)) {
            payments += 1;

            if (payments === 1) {
                return { status: 500, body: brokenBody };
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

describe("simulation F17: the backend transaction broke mid-payment", () => {
    beforeEach(() => {
        sessionStorage.clear();

        vi.useFakeTimers({ shouldAdvanceTime: true });
        vi.setSystemTime(now);

        booking.reset();

        vi.mocked(api.request).mockReset().mockResolvedValue(heldBooking);
        vi.mocked(classMutator.send).mockReset();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("behaviour: the retry carries the same key, so it cannot charge twice", async () => {
        // The one assertion this whole simulation exists for. The first call
        // came back with no answer about what happened, so the second one has
        // to be the same attempt, not a new one.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const settled = await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(settled?.status).toBe("confirmed");

        const payments = stage.transport.callsTo("POST", paymentPathFor(bookingId));

        expect(payments).toHaveLength(2);
        expect(payments[0].headers?.[idempotencyKeyHeader]).toBe(payments[1].headers?.[idempotencyKeyHeader]);
    });

    test("behaviour: the booking stays on screen as pending rather than being cleared", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const held = get(stage.booking).booking;

        expect(held?.id).toBe(bookingId);
        expect(held?.status).toBe("pending_payment");
    });

    test("behaviour: nothing is known to have changed, so the cached list is left alone", async () => {
        // Ageing the entry would claim knowledge this client does not have.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });
        await stage.classes.load();

        expect(get(stage.classes).lastResult).toBe("fresh");
        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(1);
    });

    test("behaviour: the message claims nothing about the seat or the money", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        const failure = get(stage.booking).failure;

        expect(failure?.kind).toBe("Unavailable");
        expect(failure?.message).toBe("something went wrong on our side, your booking was not changed");
        expect(failure?.message).not.toMatch(/seat|declined|refund/i);
    });

    test("behaviour: the request id is carried, so a parent has something to quote", async () => {
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.booking.pay(bookingId, { amount_cents: 4500, currency: "SGD" });

        expect(get(stage.booking).failure?.requestId).toBe(requestId);
    });

    test("behaviour: on screen the parent is offered a retry and the reference, and told nothing else", async () => {
        vi.mocked(classMutator.send).mockRejectedValue(
            new ApiError("Unavailable", "internal_error", 500, 0, requestId),
        );

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(screen.getByTestId("pay-unknown-outcome")).toBeInTheDocument();
        });

        expect(screen.getByTestId("pay-request-id")).toHaveTextContent(requestId);
        expect(screen.getByTestId("payment-submit")).toHaveTextContent("Try the payment again");
        expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        expect(screen.queryByTestId("pay-seat-lost")).not.toBeInTheDocument();
    });

    test("edge: the countdown keeps running, because the hold was not touched either", async () => {
        vi.mocked(classMutator.send).mockRejectedValue(
            new ApiError("Unavailable", "internal_error", 500, 0, requestId),
        );

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(screen.getByTestId("pay-unknown-outcome")).toBeInTheDocument();
        });

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("left to pay");
    });
});
