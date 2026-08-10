import { render, screen } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import type { Booking, BookingStatus as Status } from "$lib/api/types";
import BookingStatus from "./BookingStatus.svelte";

const bookingId = "0192a000-0000-7000-8000-000000000031";

/** One booking in whichever state the case is about. */
function bookingIn(status: Status, seatNo: number | null = null): Booking {
    return {
        id: bookingId,
        student_id: "0192a000-0000-7000-8000-000000000011",
        class_id: "0192a000-0000-7000-8000-000000000021",
        status,
        seat_no: seatNo,
        hold_expires_at: status === "pending_payment" ? "2026-08-11T09:10:00Z" : null,
    };
}

/** Every status the backend enum can hold. */
const everyStatus: Status[] = [
    "pending_payment",
    "confirmed",
    "payment_failed",
    "refund_required",
    "expired",
    "cancelled",
];

describe("the booking status", () => {
    test("integration: a confirmed booking shows the seat that was won", () => {
        render(BookingStatus, { props: { booking: bookingIn("confirmed", 3) } });

        expect(screen.getByTestId("booking-status-headline")).toHaveTextContent("confirmed");
        expect(screen.getByTestId("booking-status-seat")).toHaveTextContent("Seat 3");
    });

    test("edge: a booking with no seat shows no seat line rather than an empty one", () => {
        render(BookingStatus, { props: { booking: bookingIn("pending_payment") } });

        expect(screen.queryByTestId("booking-status-seat")).not.toBeInTheDocument();
    });

    test("behaviour: a lost seat says the money is coming back and asks for nothing", () => {
        render(BookingStatus, { props: { booking: bookingIn("refund_required") } });

        const guidance = screen.getByTestId("booking-status-guidance");

        expect(guidance).toHaveTextContent(/refunded/i);
        expect(guidance).toHaveTextContent(/nothing for you to do/i);
    });

    test("behaviour: a declined payment says no money was taken, which is the opposite case", () => {
        // The difference between these two is a parent who waits and a parent
        // who telephones.
        render(BookingStatus, { props: { booking: bookingIn("payment_failed") } });

        expect(screen.getByTestId("booking-status-guidance")).toHaveTextContent(/no money was taken/i);
    });

    test("edge: every status the api can send renders a headline and guidance", () => {
        // A status that fell through would leave a parent staring at their own
        // booking with nothing to read.
        for (const status of everyStatus) {
            const { unmount } = render(BookingStatus, { props: { booking: bookingIn(status) } });

            expect(screen.getByTestId("booking-status-headline").textContent?.trim()).not.toBe("");
            expect(screen.getByTestId("booking-status-guidance").textContent?.trim()).not.toBe("");

            unmount();
        }
    });

    test("edge: the status is on the element, so a screen above can branch without matching prose", () => {
        render(BookingStatus, { props: { booking: bookingIn("expired") } });

        expect(screen.getByTestId("booking-status")).toHaveAttribute("data-status", "expired");
    });

    test("edge: the only identifier on screen is the booking reference", () => {
        // Nothing here names the child or the class, so this screen is safe on
        // a recorded walkthrough.
        const { container } = render(BookingStatus, { props: { booking: bookingIn("confirmed", 1) } });

        const shown = container.textContent ?? "";

        expect(shown).toContain(bookingId);
        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000011");
        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000021");
    });
});
