import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { goto } from "$app/navigation";
import { ApiError } from "$lib/api/errors";
import type { Booking, BookingStatus } from "$lib/api/types";
import { requestedCancel } from "$lib/booking/cancel_request";
import { api } from "$lib/session/client";
import { booking } from "$lib/stores/booking";
import BookingPage from "./+page.svelte";

const bookingId = "0192a000-0000-7000-8000-000000000031";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$app/state", () => ({
    page: { params: { bookingId: "0192a000-0000-7000-8000-000000000031" } },
}));

vi.mock("$lib/booking/cancel_request", () => ({
    requestedCancel: vi.fn(() => Promise.resolve()),
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

/** One booking in whichever state the case is about. */
function bookingIn(status: BookingStatus, seatNo: number | null = null): Booking {
    return {
        id: bookingId,
        student_id: "0192a000-0000-7000-8000-000000000011",
        class_id: "0192a000-0000-7000-8000-000000000021",
        class_subject: "science",
        class_title: "Science Discovery",
        class_starts_at: "2026-08-15T01:28:26.224983Z",
        status,
        seat_no: seatNo,
        hold_expires_at: status === "pending_payment" ? "2026-08-11T09:10:00Z" : null,
    };
}

describe("the booking status screen", () => {
    beforeEach(() => {
        booking.reset();

        vi.mocked(goto).mockClear();
        vi.mocked(requestedCancel).mockReset().mockResolvedValue(bookingIn("cancelled"));
        vi.mocked(api.request).mockReset().mockResolvedValue(bookingIn("confirmed", 2));
    });

    test("integration: the booking is read from the api and its status is shown", async () => {
        // This screen is where a parent comes to find out what happened, so it
        // is the last place that should be showing something remembered.
        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-status")).toHaveAttribute("data-status", "confirmed");
        });

        expect(screen.getByTestId("booking-status-seat")).toHaveTextContent("Seat 2");
        expect(vi.mocked(api.request)).toHaveBeenCalledTimes(1);
    });

    test("behaviour: a booking still waiting for money offers the way back to paying", async () => {
        vi.mocked(api.request).mockResolvedValue(bookingIn("pending_payment"));

        render(BookingPage);

        const control = await screen.findByTestId("booking-pay");

        control.click();

        await waitFor(() => expect(goto).toHaveBeenCalledWith(`/pay/${bookingId}`));
    });

    test("edge: a settled booking offers no way back to paying", async () => {
        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-status")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("booking-pay")).not.toBeInTheDocument();
    });

    test("behaviour: the controls sit inside the card, not underneath it", async () => {
        render(BookingPage);

        const card = await screen.findByTestId("booking-status");

        expect(card.querySelector('[data-testid="booking-actions"]')).not.toBeNull();
    });

    test("behaviour: a hold can be given up from the screen that shows it", async () => {
        vi.mocked(api.request).mockResolvedValue(bookingIn("pending_payment"));

        render(BookingPage);

        const control = await screen.findByTestId("booking-cancel");

        control.click();

        const confirm = await screen.findByTestId("booking-cancel-confirm");

        confirm.click();

        await waitFor(() => expect(requestedCancel).toHaveBeenCalledWith(bookingId));
    });

    test("integration: a cancel that went through makes this screen read the booking again", async () => {
        // What the cancel answered is not what this screen shows. It exists to
        // say where a booking stands, and the api is what decides that.
        vi.mocked(api.request).mockResolvedValue(bookingIn("pending_payment"));

        render(BookingPage);

        const control = await screen.findByTestId("booking-cancel");

        control.click();

        (await screen.findByTestId("booking-cancel-confirm")).click();

        await waitFor(() => expect(vi.mocked(api.request)).toHaveBeenCalledTimes(2));
    });

    test("edge: a booking already finished offers no way to cancel it", async () => {
        // The api would refuse it, and a control that only ever produces a
        // refusal is a control that should not be on screen.
        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-status")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("booking-cancel")).not.toBeInTheDocument();
    });

    test("edge: a booking that cannot be read leaves a sentence rather than a blank page", async () => {
        vi.mocked(api.request).mockRejectedValue(
            new ApiError("Unavailable", "dependency_unavailable", 503),
        );

        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-failure")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("booking-status")).not.toBeInTheDocument();
    });

    test("edge: a refused read never claims the booking is gone", async () => {
        // The api could not be reached. That says nothing about the booking,
        // and a screen that guessed would be guessing about somebody's money.
        vi.mocked(api.request).mockRejectedValue(
            new ApiError("Unavailable", "internal_error", 500, 0, "request-7"),
        );

        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-failure")).toBeInTheDocument();
        });

        expect(screen.getByTestId("booking-failure").textContent).not.toMatch(/cancelled|expired|refund/i);
    });
});
