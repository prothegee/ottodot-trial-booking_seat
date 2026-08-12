import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createApiClient } from "$lib/api/client";
import { idempotencyKeyHeader } from "$lib/api/idempotency";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import type { Booking } from "$lib/api/types";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCacheStore } from "$lib/cache/store";
import { api } from "$lib/session/client";
import { classMutator } from "$lib/session/cached_api";
import { booking, bookingsPath, createBookingStore, paymentPathFor } from "$lib/stores/booking";
import PayPage from "../routes/pay/[bookingId]/+page.svelte";
import { trialPayment } from "$lib/booking/price";

/**
 * Simulation F8: the parent clicks submit twice.
 *
 *     parent -> client: click submit
 *     client -> api: pay with key K
 *     parent -> client: click submit again immediately
 *     note over client: control disabled, no second call issued
 *     api -> client: settled
 *     client -> parent: result shown once
 *
 * Asserts: exactly one call is recorded by the fake transport, the control is
 * disabled for the whole in-flight window, and the key would have made a leaked
 * second call harmless anyway.
 *
 * The last part is why this is a simulation and not just a disabled attribute.
 * The control is the cheap layer. The idempotency key is the correct one, and
 * the case below proves it holds even when the control is bypassed entirely.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

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
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: new Date(now + 10 * 60 * 1000).toISOString(),
};

const confirmedBooking: Booking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

describe("simulation F8: the submit control is clicked twice", () => {
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

    test("behaviour: a second click during the round trip issues no second call", async () => {
        // The payment is held open until the test lets it answer, which is the
        // only way to click into a window that would otherwise close in a
        // microsecond.
        let settle: (booking: Booking) => void = () => {};

        vi.mocked(classMutator.send).mockImplementation(
            () =>
                new Promise((resolve) => {
                    settle = resolve as (booking: Booking) => void;
                }),
        );

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();
        screen.getByTestId("payment-submit").click();

        expect(vi.mocked(classMutator.send)).toHaveBeenCalledTimes(1);

        settle(confirmedBooking);

        await waitFor(() => {
            expect(vi.mocked(classMutator.send)).toHaveBeenCalledTimes(1);
        });
    });

    test("behaviour: the control is dead for the whole in-flight window, not just the first instant", async () => {
        let settle: (booking: Booking) => void = () => {};

        vi.mocked(classMutator.send).mockImplementation(
            () =>
                new Promise((resolve) => {
                    settle = resolve as (booking: Booking) => void;
                }),
        );

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).toBeDisabled();
        });

        await vi.advanceTimersByTimeAsync(5000);

        expect(screen.getByTestId("payment-submit")).toBeDisabled();

        settle(confirmedBooking);
    });

    test("behaviour: a leaked second call carries the same key, so it is harmless", async () => {
        // The control is bypassed entirely here, calling the store twice the
        // way a lost click or a scripted double post would. One key covers
        // both, which is what makes the api answer once rather than charge
        // twice.
        const transport = createFakeTransport((request) => {
            if (request.method === "POST" && request.path === bookingsPath) {
                return { status: 201, body: heldBooking };
            }

            if (request.method === "POST" && request.path === paymentPathFor(bookingId)) {
                return { status: 200, body: confirmedBooking };
            }

            return { status: 404, body: errorBody("invalid_request") };
        });

        const client = createApiClient({ transport, onSignOut: () => {} });
        const store = createCacheStore();
        const inFlight = createBookingStore({
            mutator: createCacheAwareMutator({ client, store }),
            cache: store,
        });

        await inFlight.create({ student_id: studentId, class_id: classId });

        await Promise.all([
            inFlight.pay(bookingId, trialPayment()),
            inFlight.pay(bookingId, trialPayment()),
        ]);

        const payments = transport.callsTo("POST", paymentPathFor(bookingId));

        expect(payments).toHaveLength(2);
        expect(payments[0].headers?.[idempotencyKeyHeader]).toBe(payments[1].headers?.[idempotencyKeyHeader]);
    });

    test("behaviour: the result is shown once, and the parent is moved on once", async () => {
        vi.mocked(classMutator.send).mockResolvedValue(confirmedBooking);

        const { goto } = await import("$app/navigation");

        vi.mocked(goto).mockClear();

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(vi.mocked(goto)).toHaveBeenCalledWith(`/booking/${bookingId}`);
        });

        expect(vi.mocked(goto)).toHaveBeenCalledTimes(1);
    });
});
