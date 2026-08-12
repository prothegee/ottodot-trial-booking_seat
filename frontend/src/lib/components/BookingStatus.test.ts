import { render, screen } from "@testing-library/svelte";
import { createRawSnippet } from "svelte";
import { describe, expect, test } from "vitest";

import type { Booking, BookingStatus as Status } from "$lib/api/types";
import BookingStatus from "./BookingStatus.svelte";

/** Stands in for whatever controls the screen above hands the card. */
const someControls = createRawSnippet(() => ({
    render: () => `<button data-testid="some-control">do a thing</button>`,
}));

const bookingId = "0192a000-0000-7000-8000-000000000031";

/** One booking in whichever state the case is about. */
function bookingIn(status: Status, seatNo: number | null = null): Booking {
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

    test("integration: the card says which class the booking is for", () => {
        // A card that opened with "Your place is confirmed" and never named the
        // class left a parent with a reference and a seat number, and no way to
        // tell one of their bookings from another.
        render(BookingStatus, { props: { booking: bookingIn("confirmed", 3) } });

        expect(screen.getByTestId("booking-status-subject")).toHaveTextContent("science");
        expect(screen.getByTestId("booking-status-class")).toHaveTextContent("Science Discovery");
        expect(screen.getByTestId("booking-status-when").textContent?.trim()).not.toBe("");
    });

    test("behaviour: the class is the heading, so the status reads as what happened to it", () => {
        render(BookingStatus, { props: { booking: bookingIn("confirmed", 3) } });

        expect(screen.getByTestId("booking-status-class").tagName).toBe("H2");
    });

    test("edge: a booking whose class could not be read leaves the block out", () => {
        // The api answers a booking even when the class description is
        // unavailable, because that read decides nothing. An empty heading
        // above the status would be worse than no heading.
        render(BookingStatus, {
            props: {
                booking: {
                    ...bookingIn("confirmed", 3),
                    class_subject: "",
                    class_title: "",
                    class_starts_at: null,
                },
            },
        });

        expect(screen.queryByTestId("booking-status-class")).not.toBeInTheDocument();
        expect(screen.queryByTestId("booking-status-subject")).not.toBeInTheDocument();
        expect(screen.getByTestId("booking-status-headline")).toHaveTextContent("confirmed");
    });

    test("edge: with no class to name, the status becomes the card's heading", () => {
        // A card with no heading at all is one a screen reader cannot summarise.
        render(BookingStatus, {
            props: {
                booking: {
                    ...bookingIn("confirmed", 3),
                    class_subject: "",
                    class_title: "",
                    class_starts_at: null,
                },
            },
        });

        expect(screen.getByTestId("booking-status-headline").tagName).toBe("H2");
    });

    test("edge: a class with no start time still shows its name", () => {
        render(BookingStatus, {
            props: { booking: { ...bookingIn("confirmed", 3), class_starts_at: null } },
        });

        expect(screen.getByTestId("booking-status-class")).toHaveTextContent("Science Discovery");
        expect(screen.queryByTestId("booking-status-when")).not.toBeInTheDocument();
    });

    test("behaviour: controls handed to the card are rendered inside it", () => {
        // A line of links under the card reads as a footnote about the booking
        // rather than as something to do with it.
        render(BookingStatus, {
            props: { booking: bookingIn("confirmed", 3), children: someControls },
        });

        expect(screen.getByTestId("booking-status").contains(screen.getByTestId("some-control"))).toBe(
            true,
        );
    });

    test("edge: a card handed no controls renders none rather than an empty block", () => {
        render(BookingStatus, { props: { booking: bookingIn("confirmed", 3) } });

        expect(screen.getByTestId("booking-status").querySelector("button")).toBeNull();
    });

    test("edge: the only identifier on screen is the booking reference", () => {
        // Nothing here names the child or the class, so this screen is safe to
        // show to anybody.
        const { container } = render(BookingStatus, { props: { booking: bookingIn("confirmed", 1) } });

        const shown = container.textContent ?? "";

        expect(shown).toContain(bookingId);
        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000011");
        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000021");
    });
});
