/**
 * The shapes the api sends and accepts.
 *
 * The field names are the wire names, unchanged. There is no second camelCase
 * vocabulary and no mapping layer between them, on purpose: one name per field
 * means one place to change when the contract changes, and a reviewer reading a
 * component can search the backend for the same word.
 *
 * Nothing here carries an email or a token. That is not an omission, it is the
 * contract: the api never sends one, so the client can never hold one.
 */

/** What the api reports the signed-in parent is allowed to do. */
export type ParentRole = "parent" | "admin";

/** One child on the parent's account. */
export interface Child {
    id: string;
    full_name: string;
    grade_level: number;
}

/** The answer to GET /api/v1/auth/me. */
export interface Session {
    parent_id: string;
    display_name: string;
    role: ParentRole;
    children: Child[];
}

/** The body of POST /api/v1/auth/login. Mock sign in, seeded email, no password. */
export interface LoginRequest {
    email: string;
}

/**
 * One trial class, as the class list sends it.
 *
 * `seats_remaining` is advisory and nothing here may decide on it. It is what
 * saves a parent a wasted click, and by the time they click it may already be
 * wrong. The only decision in this system is a transaction on the backend.
 */
export interface TrialClass {
    id: string;
    subject: string;
    title: string;

    /** RFC 3339, as the api sends it. Formatted for display, never parsed for a rule. */
    starts_at: string;

    duration_minutes: number;
    capacity: number;
    seats_remaining: number;
}

/** The answer to GET /api/v1/classes. */
export interface ClassList {
    classes: TrialClass[];
}

/** Where a booking sits, matching the backend enum exactly. */
export type BookingStatus =
    | "pending_payment"
    | "confirmed"
    | "payment_failed"
    | "refund_required"
    | "expired"
    | "cancelled";

/**
 * One booking, as the api reports it.
 *
 * Two fields are null rather than absent when they do not apply, which is what
 * the api sends: a booking with no seat has no seat number, and a booking that
 * is not holding has no deadline.
 */
export interface Booking {
    id: string;
    student_id: string;
    class_id: string;
    status: BookingStatus;
    seat_no: number | null;
    hold_expires_at: string | null;
}

/** The body of POST /api/v1/bookings. */
export interface CreateBookingRequest {
    student_id: string;
    class_id: string;
}

/** The body of POST /api/v1/bookings/{bookingId}/payments. */
export interface PayRequest {
    amount_cents: number;
    currency: string;
}

/**
 * The one failure shape the api uses.
 *
 * The client switches on `code` and never on `message`. The prose is the
 * backend's, and this client owns its own wording, so the message is read only
 * when a support conversation needs the exact server text.
 */
export interface ErrorEnvelope {
    error: {
        code: string;
        message: string;
        retry_after_seconds?: number;
        request_id?: string;

        /**
         * The booking a failure points at, when there is one.
         *
         * Only `already_booked` carries it today. It is what lets the duplicate
         * notice link to the booking the parent already has, instead of telling
         * them one exists and leaving them to find it.
         */
        booking_id?: string;
    };
}

/**
 * Reports whether a parsed response body is the failure envelope.
 *
 * A failure that arrives without one, from a proxy or a crash before the
 * handler ran, still has to be handled, which is why this is a check rather
 * than an assumption.
 */
export function isErrorEnvelope(body: unknown): body is ErrorEnvelope {
    if (typeof body !== "object" || body === null) {
        return false;
    }

    const candidate = (body as { error?: unknown }).error;

    if (typeof candidate !== "object" || candidate === null) {
        return false;
    }

    return typeof (candidate as { code?: unknown }).code === "string";
}
