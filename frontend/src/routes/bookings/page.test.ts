import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { goto } from "$app/navigation";
import { ApiError } from "$lib/api/errors";
import type { Booking, BookingStatus } from "$lib/api/types";
import { requestedCancel } from "$lib/booking/cancel_request";
import { api } from "$lib/session/client";
import { bookings } from "$lib/stores/bookings";
import BookingsPage from "./+page.svelte";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
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
function bookingIn(id: string, status: BookingStatus, seatNo: number | null = null): Booking {
    return {
        id,
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

const waiting = bookingIn("0192a000-0000-7000-8000-000000000031", "pending_payment");
const settled = bookingIn("0192a000-0000-7000-8000-000000000032", "confirmed", 2);

describe("the bookings screen", () => {
    beforeEach(() => {
        bookings.reset();

        vi.mocked(goto).mockClear();
        vi.mocked(requestedCancel).mockReset().mockResolvedValue(waiting);
        vi.mocked(api.request).mockReset().mockResolvedValue({ bookings: [waiting, settled] });
    });

    test("integration: the list is read from the api and every booking is shown", async () => {
        render(BookingsPage);

        await waitFor(() => {
            expect(screen.getAllByTestId("bookings-row")).toHaveLength(2);
        });

        expect(vi.mocked(api.request)).toHaveBeenCalledTimes(1);
        expect(vi.mocked(api.request)).toHaveBeenCalledWith({
            method: "GET",
            path: "/api/v1/bookings",
        });
    });

    test("behaviour: a payment still pending and one that completed are told apart", async () => {
        // This is the whole reason the screen exists. A list that showed both
        // the same way would leave a parent no better off than a closed tab.
        render(BookingsPage);

        await waitFor(() => {
            expect(screen.getAllByTestId("booking-status")).toHaveLength(2);
        });

        const cards = screen.getAllByTestId("booking-status");

        expect(cards[0]).toHaveAttribute("data-status", "pending_payment");
        expect(cards[1]).toHaveAttribute("data-status", "confirmed");
        expect(screen.getByTestId("booking-status-seat")).toHaveTextContent("Seat 2");
    });

    test("behaviour: every row opens the booking it names", async () => {
        render(BookingsPage);

        const controls = await screen.findAllByTestId("booking-open");

        controls[1].click();

        await waitFor(() => expect(goto).toHaveBeenCalledWith(`/booking/${settled.id}`));
    });

    test("behaviour: every row says which class it is for", async () => {
        // The list is the one screen where two bookings sit next to each other,
        // so it is where an unnamed card costs the most.
        render(BookingsPage);

        const named = await screen.findAllByTestId("booking-status-class");

        expect(named).toHaveLength(2);
        expect(named[0]).toHaveTextContent("Science Discovery");
    });

    test("behaviour: the controls sit inside the card, not underneath it", async () => {
        // A line of links under the card reads as a footnote about the booking
        // rather than as something to do with it.
        render(BookingsPage);

        const cards = await screen.findAllByTestId("booking-status");

        expect(cards[0].querySelector('[data-testid="booking-actions"]')).not.toBeNull();
    });

    test("behaviour: a booking still holding can be withdrawn from the list", async () => {
        // The hold is the only booking a parent can give up, and this list is
        // where they find it after closing the tab they made it in.
        render(BookingsPage);

        await waitFor(() => expect(screen.getAllByTestId("bookings-row")).toHaveLength(2));

        expect(screen.getAllByTestId("booking-cancel")).toHaveLength(1);
    });

    test("integration: a cancel that went through makes the list read itself again", async () => {
        vi.mocked(requestedCancel).mockResolvedValue({ ...waiting, status: "cancelled" });

        render(BookingsPage);

        await waitFor(() => expect(screen.getAllByTestId("bookings-row")).toHaveLength(2));

        screen.getByTestId("booking-cancel").click();

        const confirm = await screen.findByTestId("booking-cancel-confirm");

        confirm.click();

        await waitFor(() => expect(vi.mocked(api.request)).toHaveBeenCalledTimes(2));
    });

    test("edge: a parent who has booked nothing is told so rather than shown a blank page", async () => {
        vi.mocked(api.request).mockResolvedValue({ bookings: [] });

        render(BookingsPage);

        await waitFor(() => {
            expect(screen.getByTestId("bookings-empty")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("bookings-list")).not.toBeInTheDocument();
    });

    test("edge: a list that cannot be read leaves a sentence rather than an empty state", async () => {
        // "you have not booked anything" and "the list could not be loaded" are
        // opposite messages, and showing the first one for the second reason
        // tells a parent their booking is gone.
        vi.mocked(api.request).mockRejectedValue(
            new ApiError("Unavailable", "dependency_unavailable", 503),
        );

        render(BookingsPage);

        await waitFor(() => {
            expect(screen.getByTestId("bookings-failure")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("bookings-empty")).not.toBeInTheDocument();
    });

    test("edge: a failed read leaves the list already on screen alone", async () => {
        render(BookingsPage);

        await waitFor(() => {
            expect(screen.getAllByTestId("bookings-row")).toHaveLength(2);
        });

        vi.mocked(api.request).mockRejectedValue(
            new ApiError("Unavailable", "dependency_unavailable", 503),
        );

        await bookings.load();

        await waitFor(() => {
            expect(screen.getByTestId("bookings-failure")).toBeInTheDocument();
        });

        expect(screen.getAllByTestId("bookings-row")).toHaveLength(2);
    });
});
