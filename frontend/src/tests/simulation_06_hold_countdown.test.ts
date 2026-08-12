import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Booking } from "$lib/api/types";
import { remainingFor } from "$lib/booking/countdown";
import { api } from "$lib/session/client";
import { classMutator } from "$lib/session/cached_api";
import { booking } from "$lib/stores/booking";
import PayPage from "../routes/pay/[bookingId]/+page.svelte";

/**
 * Test 6: the hold countdown reaches zero.
 *
 *     mount[payment screen mounts with a deadline] --> tick[countdown ticks]
 *     tick --> zero{deadline reached}
 *     zero -->|no| tick
 *     zero -->|yes| lock[payment control disabled]
 *     lock --> check[ask the api for the booking status]
 *     check --> expired[expired, offer to start again]
 *
 * Asserts: the control disables at zero without waiting for a server round
 * trip, and the screen still confirms the real status with the api rather than
 * assuming it.
 *
 * Both halves matter and they pull in opposite directions. Waiting for the api
 * before disabling would leave a live button on a hold that has gone. Trusting
 * the browser's clock and declaring the booking expired would be this client
 * deciding something only the backend decides.
 */

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
    student_id: "0192a000-0000-7000-8000-000000000011",
    class_id: "0192a000-0000-7000-8000-000000000021",
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: new Date(now + 60 * 1000).toISOString(),
};

/** What the api reports once the worker has swept the hold. */
const expiredBooking: Booking = { ...heldBooking, status: "expired", hold_expires_at: null };

describe("test 6: the hold countdown reaches zero", () => {
    beforeEach(() => {
        vi.useFakeTimers({ shouldAdvanceTime: true });
        vi.setSystemTime(now);

        booking.reset();

        vi.mocked(api.request).mockReset().mockResolvedValue(heldBooking);
        vi.mocked(classMutator.send).mockReset();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("unit: the countdown counts down to zero and stops there", () => {
        const deadline = heldBooking.hold_expires_at;

        expect(remainingFor(deadline, now).label).toBe("01:00");
        expect(remainingFor(deadline, now + 30 * 1000).label).toBe("00:30");
        expect(remainingFor(deadline, now + 60 * 1000).expired).toBe(true);
        expect(remainingFor(deadline, now + 90 * 1000).label).toBe("00:00");
    });

    test("behaviour: the control disables at zero without waiting for the api", async () => {
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        // The api is made unreachable from here on, so nothing that follows can
        // be explained by a server answer.
        vi.mocked(api.request).mockRejectedValue(new ApiError("Unavailable", "dependency_unavailable", 503));

        vi.setSystemTime(now + 61 * 1000);

        await vi.advanceTimersByTimeAsync(1000);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).toBeDisabled();
        });
    });

    test("behaviour: the screen still asks the api what really happened", async () => {
        // A clock in a browser is not what decides. It closes the control, and
        // then the one place that knows is asked.
        render(PayPage);

        await waitFor(() => {
            expect(vi.mocked(api.request)).toHaveBeenCalledTimes(1);
        });

        vi.mocked(api.request).mockResolvedValue(expiredBooking);

        vi.setSystemTime(now + 61 * 1000);

        await vi.advanceTimersByTimeAsync(1000);

        await waitFor(() => {
            expect(vi.mocked(api.request).mock.calls.length).toBeGreaterThan(1);
        });
    });

    test("behaviour: once the api confirms it expired, the parent is offered the way onward", async () => {
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        vi.mocked(api.request).mockResolvedValue(expiredBooking);

        vi.setSystemTime(now + 61 * 1000);

        await vi.advanceTimersByTimeAsync(1000);

        await waitFor(() => {
            expect(screen.getByTestId("pay-not-holding")).toBeInTheDocument();
        });

        expect(screen.getByRole("link", { name: /where this booking stands/i })).toHaveAttribute(
            "href",
            `/booking/${bookingId}`,
        );
    });

    test("edge: the api is asked once, not once per tick", async () => {
        // What reaching zero does is ask a question. Asking it every second
        // would be a poll nobody asked for.
        render(PayPage);

        await waitFor(() => {
            expect(vi.mocked(api.request)).toHaveBeenCalledTimes(1);
        });

        vi.setSystemTime(now + 61 * 1000);

        await vi.advanceTimersByTimeAsync(10 * 1000);

        expect(vi.mocked(api.request)).toHaveBeenCalledTimes(2);
    });

    test("edge: a hold that had already ended before the screen opened closes it at once", async () => {
        // A parent opening the link an hour later must not be shown a live
        // payment button.
        vi.mocked(api.request).mockResolvedValue({
            ...heldBooking,
            hold_expires_at: new Date(now - 60 * 1000).toISOString(),
        });

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).toBeDisabled();
        });

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("Your hold has ended");
    });
});
