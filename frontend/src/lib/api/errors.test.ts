import { describe, expect, test } from "vitest";

import { ApiError, messageForKind, toApiError, type ApiErrorKind } from "$lib/api/errors";
import { errorBody } from "$lib/api/transport_fake";

/**
 * The backend error table, written out by hand.
 *
 * A second copy on purpose. A test that asks the mapping what it maps and then
 * agrees with the answer proves nothing, so this list mirrors the table in the
 * plan and has to be changed deliberately.
 */
const backendTable: Array<{ status: number; code: string; kind: ApiErrorKind }> = [
    { status: 401, code: "token_expired", kind: "SignedOut" },
    { status: 401, code: "token_invalid", kind: "SignedOut" },
    { status: 401, code: "token_reused", kind: "SignedOut" },
    { status: 403, code: "not_your_child", kind: "NotYourChild" },
    { status: 403, code: "forbidden_role", kind: "Forbidden" },
    { status: 409, code: "already_booked", kind: "AlreadyBooked" },
    { status: 409, code: "too_many_holds", kind: "TooManyHolds" },
    { status: 409, code: "class_full", kind: "ClassFull" },
    { status: 409, code: "seat_lost", kind: "SeatLost" },
    { status: 429, code: "rate_limited", kind: "RateLimited" },
    { status: 402, code: "payment_declined", kind: "PaymentDeclined" },
    { status: 400, code: "invalid_request", kind: "InvalidRequest" },
    { status: 500, code: "internal_error", kind: "Unavailable" },
    { status: 503, code: "dependency_unavailable", kind: "Unavailable" },
];

describe("mapping an api failure", () => {
    test("unit: every status and code pair maps to its typed error", () => {
        for (const entry of backendTable) {
            const failure = toApiError(entry.status, errorBody(entry.code));

            expect(failure).toBeInstanceOf(ApiError);
            expect(failure.kind).toBe(entry.kind);
            expect(failure.code).toBe(entry.code);
            expect(failure.status).toBe(entry.status);
        }
    });

    test("edge: an unknown code falls back to a generic error rather than throwing", () => {
        // A backend that grows a code this build has never seen must still
        // leave the parent with a screen they can act on.
        const failure = toApiError(418, errorBody("teapot_overheated"));

        expect(failure.kind).toBe("Unavailable");
        expect(failure.code).toBe("teapot_overheated");
    });

    test("edge: a failure with no envelope at all is still an ApiError", () => {
        // A proxy returning html, or a crash before the handler ran. Neither
        // may turn into an exception the caller has to guess about.
        const failure = toApiError(502, "<html>Bad Gateway</html>");

        expect(failure.kind).toBe("Unavailable");
        expect(failure.status).toBe(502);
    });

    test("edge: an envelope missing its code is treated as no envelope", () => {
        const failure = toApiError(500, { error: { message: "something" } });

        expect(failure.kind).toBe("Unavailable");
    });

    test("unit: retry_after_seconds is carried through for a rate limit", () => {
        const failure = toApiError(429, errorBody("rate_limited", "slow down", 30));

        expect(failure.retryAfterSeconds).toBe(30);
    });

    test("edge: a failure with no retry hint reports zero, never undefined", () => {
        const failure = toApiError(409, errorBody("class_full"));

        expect(failure.retryAfterSeconds).toBe(0);
    });
});

describe("what a parent reads", () => {
    test("unit: the message comes from this client, never from the server", () => {
        const failure = toApiError(409, errorBody("class_full", "PG_ERR_42 class 0192a0 is full"));

        expect(failure.message).toBe(messageForKind.ClassFull);
        expect(failure.message).not.toContain("PG_ERR");
    });

    test("edge: no message names a class, a child, or an identifier", () => {
        // These strings are on screen for the whole of a demonstration.
        for (const message of Object.values(messageForKind)) {
            expect(message).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}/);
            expect(message).not.toMatch(/@/);
            expect(message.length).toBeGreaterThan(0);
        }
    });

    test("edge: internal_error says nothing about the booking", () => {
        // The client cannot know whether the seat was lost or the payment
        // failed, so the wording must not guess at either.
        const failure = toApiError(500, errorBody("internal_error"));

        expect(failure.message).not.toMatch(/seat/i);
        expect(failure.message).not.toMatch(/payment/i);
    });

    test("unit: the request id is carried so it can be quoted to support", () => {
        const failure = toApiError(500, {
            error: { code: "internal_error", message: "boom", request_id: "req-123" },
        });

        expect(failure.requestId).toBe("req-123");
    });
});
