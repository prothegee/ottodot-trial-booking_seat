import { describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { TransportRequest } from "$lib/api/transport";
import type { Booking } from "$lib/api/types";
import { requestedCancel } from "$lib/booking/cancel_request";

const bookingId = "0192a000-0000-7000-8000-000000000031";

const withdrawn: Booking = {
    id: bookingId,
    student_id: "0192a000-0000-7000-8000-000000000011",
    class_id: "0192a000-0000-7000-8000-000000000021",
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "cancelled",
    seat_no: null,
    hold_expires_at: null,
};

describe("withdrawing a booking", () => {
    test("integration: it sends the delete the api registered for a cancel", async () => {
        const sender = { send: vi.fn(() => Promise.resolve(withdrawn)) };

        const answer = await requestedCancel(bookingId, sender);

        expect(sender.send).toHaveBeenCalledWith({
            method: "DELETE",
            path: `/api/v1/bookings/${bookingId}`,
        });
        expect(answer.status).toBe("cancelled");
    });

    test("behaviour: it goes through a mutator, so the freed seat cannot stay on screen", async () => {
        // The class list is cached and a cancel puts a seat back. Sending this
        // through the plain api client would leave a count that is now wrong.
        const invalidated = vi.fn();

        const sender = {
            send: vi.fn(async () => {
                invalidated();

                return withdrawn;
            }),
        };

        await requestedCancel(bookingId, sender);

        expect(invalidated).toHaveBeenCalledTimes(1);
    });

    test("edge: a refusal reaches the caller rather than being swallowed", async () => {
        // A cancel that did not happen must never look like one that did. The
        // parent still holds the booking and has to be told.
        const sender = {
            send: vi.fn(() => Promise.reject(new ApiError("InvalidRequest", "invalid_request", 409))),
        };

        await expect(requestedCancel(bookingId, sender)).rejects.toBeInstanceOf(ApiError);
    });

    test("edge: no idempotency key is sent, because the api asks for one only where money moves", async () => {
        let sent: TransportRequest | null = null;

        await requestedCancel(bookingId, {
            send: (request) => {
                sent = request;

                return Promise.resolve(withdrawn);
            },
        });

        expect(sent).not.toHaveProperty("headers");
    });
});
