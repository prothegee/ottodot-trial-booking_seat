import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { goto } from "$app/navigation";
import { ApiError } from "$lib/api/errors";
import type { Booking, BookingStatus as Status } from "$lib/api/types";
import { requestedCancel } from "$lib/booking/cancel_request";
import BookingActions from "./BookingActions.svelte";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

// The call itself is cancel_request.test.ts. What is under test here is the
// control: who is offered it, how many presses it takes, and what a refusal
// leaves on screen.
vi.mock("$lib/booking/cancel_request", () => ({
    requestedCancel: vi.fn(() => Promise.resolve()),
}));

const bookingId = "0192a000-0000-7000-8000-000000000031";

/** One booking in whichever state the case is about. */
function bookingIn(status: Status): Booking {
    return {
        id: bookingId,
        student_id: "0192a000-0000-7000-8000-000000000011",
        class_id: "0192a000-0000-7000-8000-000000000021",
        class_subject: "science",
        class_title: "Science Discovery",
        class_starts_at: "2026-08-15T01:28:26.224983Z",
        status,
        seat_no: status === "confirmed" ? 2 : null,
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

describe("the controls on a booking card", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        vi.mocked(requestedCancel).mockReset().mockResolvedValue(bookingIn("cancelled"));
    });

    test("unit: the way in to a booking is a button, not a link", () => {
        // It sat under the card as body text, which reads as a footnote about
        // the booking rather than as the way onward.
        render(BookingActions, { props: { booking: bookingIn("confirmed"), showOpen: true } });

        expect(screen.getByTestId("booking-open").tagName).toBe("BUTTON");
    });

    test("integration: pressing it opens that booking's own screen", async () => {
        render(BookingActions, { props: { booking: bookingIn("confirmed"), showOpen: true } });

        screen.getByTestId("booking-open").click();

        await waitFor(() => expect(goto).toHaveBeenCalledWith(`/booking/${bookingId}`));
    });

    test("edge: the screen a booking is already on does not offer a way to itself", () => {
        render(BookingActions, { props: { booking: bookingIn("confirmed") } });

        expect(screen.queryByTestId("booking-open")).not.toBeInTheDocument();
    });

    test("integration: a booking still holding offers the payment screen", async () => {
        render(BookingActions, { props: { booking: bookingIn("pending_payment"), showPay: true } });

        screen.getByTestId("booking-pay").click();

        await waitFor(() => expect(goto).toHaveBeenCalledWith(`/pay/${bookingId}`));
    });

    test("edge: a booking that stopped holding is offered no payment screen", () => {
        render(BookingActions, { props: { booking: bookingIn("expired"), showPay: true } });

        expect(screen.queryByTestId("booking-pay")).not.toBeInTheDocument();
    });

    test("behaviour: only a hold can be cancelled here", () => {
        // A confirmed booking took money and the api sends none of it back on a
        // cancel. The control there would be a button that quietly costs a
        // parent the fee.
        for (const status of everyStatus) {
            const { unmount } = render(BookingActions, { props: { booking: bookingIn(status) } });

            const offered = screen.queryByTestId("booking-cancel") !== null;

            expect(offered).toBe(status === "pending_payment");

            unmount();
        }
    });

    test("behaviour: one press asks, and nothing is sent until the second", async () => {
        render(BookingActions, { props: { booking: bookingIn("pending_payment") } });

        screen.getByTestId("booking-cancel").click();

        await screen.findByTestId("booking-cancel-question");

        expect(requestedCancel).not.toHaveBeenCalled();

        screen.getByTestId("booking-cancel-confirm").click();

        await waitFor(() => expect(requestedCancel).toHaveBeenCalledWith(bookingId));
    });

    test("integration: keeping the booking sends nothing and closes the question", async () => {
        render(BookingActions, { props: { booking: bookingIn("pending_payment") } });

        screen.getByTestId("booking-cancel").click();

        await screen.findByTestId("booking-cancel-keep");

        screen.getByTestId("booking-cancel-keep").click();

        await waitFor(() => {
            expect(screen.queryByTestId("booking-cancel-question")).not.toBeInTheDocument();
        });

        expect(requestedCancel).not.toHaveBeenCalled();
    });

    test("integration: a cancel that went through tells the screen above to read again", async () => {
        const cancelled = vi.fn();

        render(BookingActions, { props: { booking: bookingIn("pending_payment"), onCancelled: cancelled } });

        screen.getByTestId("booking-cancel").click();

        await screen.findByTestId("booking-cancel-confirm");

        screen.getByTestId("booking-cancel-confirm").click();

        await waitFor(() => expect(cancelled).toHaveBeenCalledTimes(1));
    });

    test("edge: a refused cancel says so and does not claim the booking changed", async () => {
        const cancelled = vi.fn();

        vi.mocked(requestedCancel).mockRejectedValue(
            new ApiError("Unavailable", "dependency_unavailable", 503),
        );

        render(BookingActions, { props: { booking: bookingIn("pending_payment"), onCancelled: cancelled } });

        screen.getByTestId("booking-cancel").click();

        await screen.findByTestId("booking-cancel-confirm");

        screen.getByTestId("booking-cancel-confirm").click();

        const failure = await screen.findByTestId("booking-cancel-failure");

        expect(failure.textContent).toContain("was not changed");
        expect(cancelled).not.toHaveBeenCalled();
    });

    test("edge: a booking that already moved on is not described as a bad form", async () => {
        // The api answers that with invalid_request, whose shared wording talks
        // about a form. There is no form here, only a hold that ran out.
        vi.mocked(requestedCancel).mockRejectedValue(
            new ApiError("InvalidRequest", "invalid_request", 409),
        );

        render(BookingActions, { props: { booking: bookingIn("pending_payment") } });

        screen.getByTestId("booking-cancel").click();

        await screen.findByTestId("booking-cancel-confirm");

        screen.getByTestId("booking-cancel-confirm").click();

        const failure = await screen.findByTestId("booking-cancel-failure");

        expect(failure.textContent).toContain("already moved on");
        expect(failure.textContent).not.toContain("form");
    });

    test("edge: a second press while the first is running is ignored", async () => {
        // Two deletes at one booking, and the api answers the second as a
        // booking that already moved on. That refusal is correct and would
        // reach the parent as though their cancel had failed.
        let releaseFirst = (): void => {};

        vi.mocked(requestedCancel).mockImplementationOnce(
            () =>
                new Promise((resolve) => {
                    releaseFirst = () => resolve(bookingIn("cancelled"));
                }),
        );

        render(BookingActions, { props: { booking: bookingIn("pending_payment") } });

        screen.getByTestId("booking-cancel").click();

        const confirm = await screen.findByTestId("booking-cancel-confirm");

        confirm.click();

        await waitFor(() => expect(confirm).toBeDisabled());

        confirm.click();

        expect(requestedCancel).toHaveBeenCalledTimes(1);

        releaseFirst();
    });

    test("edge: the control says what it is doing while it runs", async () => {
        let releaseFirst = (): void => {};

        vi.mocked(requestedCancel).mockImplementationOnce(
            () =>
                new Promise((resolve) => {
                    releaseFirst = () => resolve(bookingIn("cancelled"));
                }),
        );

        render(BookingActions, { props: { booking: bookingIn("pending_payment") } });

        screen.getByTestId("booking-cancel").click();

        const confirm = await screen.findByTestId("booking-cancel-confirm");

        confirm.click();

        await waitFor(() => expect(confirm).toHaveTextContent("Cancelling"));

        releaseFirst();
    });

    test("edge: nothing here names the child or the class", () => {
        const { container } = render(BookingActions, {
            props: { booking: bookingIn("pending_payment"), showOpen: true, showPay: true },
        });

        const shown = container.textContent ?? "";

        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000011");
        expect(shown).not.toContain("0192a000-0000-7000-8000-000000000021");
    });
});
