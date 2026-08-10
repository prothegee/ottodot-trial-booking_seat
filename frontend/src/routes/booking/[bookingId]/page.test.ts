import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Booking, BookingStatus } from "$lib/api/types";
import { api } from "$lib/session/client";
import { booking } from "$lib/stores/booking";
import BookingPage from "./+page.svelte";

const bookingId = "0192a000-0000-7000-8000-000000000031";

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

/** One booking in whichever state the case is about. */
function bookingIn(status: BookingStatus, seatNo: number | null = null): Booking {
    return {
        id: bookingId,
        student_id: "0192a000-0000-7000-8000-000000000011",
        class_id: "0192a000-0000-7000-8000-000000000021",
        status,
        seat_no: seatNo,
        hold_expires_at: status === "pending_payment" ? "2026-08-11T09:10:00Z" : null,
    };
}

describe("the booking status screen", () => {
    beforeEach(() => {
        booking.reset();

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

        const link = await screen.findByRole("link", { name: /payment screen/i });

        expect(link).toHaveAttribute("href", `/pay/${bookingId}`);
    });

    test("edge: a settled booking offers no way back to paying", async () => {
        render(BookingPage);

        await waitFor(() => {
            expect(screen.getByTestId("booking-status")).toBeInTheDocument();
        });

        expect(screen.queryByRole("link", { name: /payment screen/i })).not.toBeInTheDocument();
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
