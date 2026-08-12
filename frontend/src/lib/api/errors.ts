/**
 * Every failure the api can report, as something the user interface can switch
 * on.
 *
 * Two rules hold this file together. The client never renders server prose,
 * because the wording on screen belongs to this client. And an unrecognised
 * code never throws, because a backend that grows a new code must not turn into
 * a blank page.
 */
import { isErrorEnvelope } from "$lib/api/types";

/** The kinds a screen can branch on. */
export type ApiErrorKind =
    | "SignedOut"
    | "NotYourChild"
    | "Forbidden"
    | "AlreadyBooked"
    | "TooManyHolds"
    | "ClassFull"
    | "RateLimited"
    | "PaymentDeclined"
    | "SeatLost"
    | "InvalidRequest"
    | "Unavailable";

/** The code a 401 uses when the access token simply aged out. */
export const tokenExpiredCode = "token_expired";

/** The code a 401 uses when a revoked refresh token was presented. */
export const tokenReusedCode = "token_reused";

/**
 * Wire code to kind, matching the backend error table one to one.
 *
 * `token_expired` is here as SignedOut for the case where it survives a refresh
 * and a retry. On its first appearance the api client handles it silently and a
 * parent never learns it happened.
 */
const kindByCode: Readonly<Record<string, ApiErrorKind>> = {
    token_expired: "SignedOut",
    token_invalid: "SignedOut",
    token_reused: "SignedOut",
    not_your_child: "NotYourChild",
    forbidden_role: "Forbidden",
    already_booked: "AlreadyBooked",
    too_many_holds: "TooManyHolds",
    class_full: "ClassFull",
    rate_limited: "RateLimited",
    payment_declined: "PaymentDeclined",
    seat_lost: "SeatLost",
    invalid_request: "InvalidRequest",
    internal_error: "Unavailable",
    dependency_unavailable: "Unavailable",
};

/**
 * What a parent reads for each kind.
 *
 * None of these names a class, a child, or an identifier, so any of them can be
 * shown on a shared screen.
 */
export const messageForKind: Readonly<Record<ApiErrorKind, string>> = {
    SignedOut: "your session ended, sign in again",
    NotYourChild: "that child is not on your account",
    Forbidden: "this page is not available on your account",
    AlreadyBooked: "this child already has a booking for this class",
    TooManyHolds: "finish or cancel an existing booking first",
    ClassFull: "this class filled while you were choosing",
    RateLimited: "too many attempts, wait a moment and try again",
    PaymentDeclined: "the payment was declined, no money was taken",
    SeatLost: "the last seat went to someone else, a refund is on the way",
    InvalidRequest: "something in the form was not accepted, check it and try again",
    Unavailable: "something went wrong on our side, your booking was not changed",
};

/** A failure from the api, already turned into something a screen can use. */
export class ApiError extends Error {
    readonly kind: ApiErrorKind;
    readonly code: string;
    readonly status: number;
    readonly retryAfterSeconds: number;
    readonly requestId: string;

    /**
     * The booking this failure points at, or an empty string.
     *
     * Only `already_booked` carries one. It is what turns "this child already
     * has a booking" into a link to that booking, which is the difference
     * between a notice and a dead end.
     */
    readonly bookingId: string;

    /**
     * The parsed response body, whatever arrived.
     *
     * It is kept because not every failing status carries the error envelope.
     * `/readyz` answers 503 with a report naming which dependency is down, and
     * that report is the only thing the status screen exists to show. Throwing
     * it away because the status was a failure would lose the answer along with
     * the failure.
     *
     * Nothing renders it. A screen reads a typed field off it or ignores it.
     */
    readonly body: unknown;

    constructor(
        kind: ApiErrorKind,
        code: string,
        status: number,
        retryAfterSeconds = 0,
        requestId = "",
        bookingId = "",
        body: unknown = undefined,
    ) {
        super(messageForKind[kind]);

        this.name = "ApiError";
        this.kind = kind;
        this.code = code;
        this.status = status;
        this.retryAfterSeconds = retryAfterSeconds;
        this.requestId = requestId;
        this.bookingId = bookingId;
        this.body = body;
    }
}

/**
 * Turns a failed response into an ApiError.
 *
 * Note:
 * - an unknown code becomes Unavailable rather than throwing. A backend that
 *   grows a code this build has never seen still leaves the parent with a
 *   screen they can act on.
 * - a response with no envelope at all, from a proxy or a crash before the
 *   handler ran, is treated the same way.
 *
 * Param:
 * status - number (the http status)
 * body - unknown (the parsed response body, whatever arrived)
 *
 * Return:
 * - an ApiError carrying the kind, the original code, and the status
 */
export function toApiError(status: number, body: unknown): ApiError {
    if (!isErrorEnvelope(body)) {
        return new ApiError("Unavailable", "missing_envelope", status, 0, "", "", body);
    }

    const envelope = body.error;
    const kind = kindByCode[envelope.code] ?? "Unavailable";

    return new ApiError(
        kind,
        envelope.code,
        status,
        envelope.retry_after_seconds ?? 0,
        envelope.request_id ?? "",
        envelope.booking_id ?? "",
        body,
    );
}
