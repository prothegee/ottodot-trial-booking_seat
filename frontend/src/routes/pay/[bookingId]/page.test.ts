import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { goto } from "$app/navigation";
import { ApiError } from "$lib/api/errors";
import type { Booking } from "$lib/api/types";
import { api } from "$lib/session/client";
import { classMutator } from "$lib/session/cached_api";
import { booking } from "$lib/stores/booking";
import PayPage from "./+page.svelte";

const bookingId = "0192a000-0000-7000-8000-000000000031";

/** The instant every case runs at, so the countdown is not a moving target. */
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
    hold_expires_at: new Date(now + 10 * 60 * 1000).toISOString(),
};

const confirmedBooking: Booking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** Waits for the booking to be on screen, then submits the payment. */
async function submitPayment(): Promise<void> {
    await waitFor(() => {
        expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
    });

    screen.getByTestId("payment-submit").click();
}

describe("the payment screen", () => {
    beforeEach(() => {
        vi.useFakeTimers({ shouldAdvanceTime: true });
        vi.setSystemTime(now);

        booking.reset();

        vi.mocked(goto).mockClear();
        vi.mocked(api.request).mockReset().mockResolvedValue(heldBooking);
        vi.mocked(classMutator.send).mockReset();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("integration: the booking is read on arrival and the countdown is shown", async () => {
        // A parent who opened this link in a second tab, or came back an hour
        // later, has to see the real state and not the one this tab remembers.
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("10:00 left to pay");
        });

        expect(vi.mocked(api.request)).toHaveBeenCalledTimes(1);
    });

    test("behaviour: a settled payment moves the parent to the booking screen", async () => {
        vi.mocked(classMutator.send).mockResolvedValue(confirmedBooking);

        render(PayPage);

        await submitPayment();

        await waitFor(() => {
            expect(vi.mocked(goto)).toHaveBeenCalledWith(`/booking/${bookingId}`);
        });
    });

    test("behaviour: a decline leaves the parent on this screen with a retry", async () => {
        // The hold is still standing, so sending them back to the class list
        // would make them start over for no reason.
        vi.mocked(classMutator.send).mockRejectedValue(
            new ApiError("PaymentDeclined", "payment_declined", 402),
        );

        render(PayPage);

        await submitPayment();

        await waitFor(() => {
            expect(screen.getByTestId("pay-failure")).toHaveTextContent("declined");
        });

        expect(vi.mocked(goto)).not.toHaveBeenCalled();
        expect(screen.getByTestId("payment-submit")).toHaveTextContent("Try the payment again");
    });

    test("behaviour: a lost seat is terminal and offers no retry", async () => {
        // The money moved and is already coming back. A button here would
        // invite the parent to pay twice.
        vi.mocked(classMutator.send).mockRejectedValue(new ApiError("SeatLost", "seat_lost", 409));

        render(PayPage);

        await submitPayment();

        await waitFor(() => {
            expect(screen.getByTestId("pay-seat-lost")).toBeInTheDocument();
        });

        expect(screen.getByTestId("payment-submit")).toBeDisabled();
    });

    test("behaviour: an internal_error claims nothing and renders the request id", async () => {
        // It never says the seat was lost and never says the payment failed,
        // because neither is known from here.
        vi.mocked(classMutator.send).mockRejectedValue(
            new ApiError("Unavailable", "internal_error", 500, 0, "request-42"),
        );

        render(PayPage);

        await submitPayment();

        await waitFor(() => {
            expect(screen.getByTestId("pay-unknown-outcome")).toBeInTheDocument();
        });

        expect(screen.getByTestId("pay-request-id")).toHaveTextContent("request-42");
        expect(screen.getByTestId("pay-unknown-outcome")).toHaveTextContent(/not been changed/i);
        expect(screen.queryByTestId("pay-seat-lost")).not.toBeInTheDocument();
    });

    test("behaviour: the countdown reaching zero closes the control and asks the api", async () => {
        // The control closes without waiting for anything, so a parent cannot
        // submit into a hold that has gone. Then the api is asked what really
        // happened, because a clock in a browser is not what decides.
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        vi.setSystemTime(now + 11 * 60 * 1000);

        await vi.advanceTimersByTimeAsync(1000);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).toBeDisabled();
        });

        expect(vi.mocked(api.request).mock.calls.length).toBeGreaterThan(1);
    });

    test("edge: a booking that is no longer waiting for money cannot be paid for", async () => {
        vi.mocked(api.request).mockResolvedValue(confirmedBooking);

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("pay-not-holding")).toBeInTheDocument();
        });

        expect(screen.getByTestId("payment-submit")).toBeDisabled();
        expect(screen.queryByTestId("hold-countdown")).not.toBeInTheDocument();
    });

    test("edge: a booking that cannot be read shows a sentence rather than a payment form", async () => {
        vi.mocked(api.request).mockRejectedValue(
            new ApiError("Unavailable", "dependency_unavailable", 503),
        );

        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("pay-loading")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("payment-form")).not.toBeInTheDocument();
    });
});
