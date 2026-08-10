/**
 * How long a hold has left, as arithmetic with no component around it.
 *
 * It is its own file because the countdown is the one piece of this screen that
 * can be got wrong silently: a negative remainder rendered as "-00:03 left", a
 * deadline the browser could not parse rendered as "NaN", a control that stays
 * live one tick past zero. All three are single values, and all three are
 * asserted here rather than through a rendered component.
 *
 * Nothing here decides anything either. Reaching zero disables a control so a
 * parent is not left submitting into a hold that has gone, and the api is still
 * what says whether the hold really expired.
 */

/** What is left of a hold at one instant. */
export interface Remaining {
    /** Milliseconds left, never negative. */
    milliseconds: number;

    /** True once the deadline has been reached or passed. */
    expired: boolean;

    /** mm:ss, ready to render. "00:00" once expired. */
    label: string;
}

/** What a booking with no deadline at all reads as. */
const noDeadline: Remaining = { milliseconds: 0, expired: true, label: "00:00" };

/**
 * Works out what is left of a hold.
 *
 * Note:
 * - a deadline already in the past reads as zero, never as a negative. A
 *   parent seeing "-01:12 left" learns nothing except that this was not
 *   thought about.
 * - the boundary is inclusive: a deadline landing on this exact instant is
 *   already expired, matching the backend, where a hold ending now is one the
 *   worker may already have swept.
 * - a deadline that is absent or unparseable reads as expired rather than as
 *   an error. The api sends null for a booking that is not holding, and a
 *   screen that threw on it would show a blank page instead of a status.
 *
 * Param:
 * deadline - string | null (the hold_expires_at the api sent, RFC 3339)
 * now - number (the instant to measure from, in milliseconds)
 *
 * Return:
 * - the milliseconds left, whether it has run out, and the label to render
 */
export function remainingFor(deadline: string | null, now: number): Remaining {
    if (deadline === null || deadline === "") {
        return noDeadline;
    }

    const endsAt = Date.parse(deadline);

    if (Number.isNaN(endsAt)) {
        return noDeadline;
    }

    const left = endsAt - now;

    if (left <= 0) {
        return noDeadline;
    }

    return { milliseconds: left, expired: false, label: labelFor(left) };
}

/**
 * Renders milliseconds as mm:ss.
 *
 * The seconds are rounded up, so a hold with 400 milliseconds left reads as one
 * second rather than as zero. Reading "00:00" while the control is still live
 * is the one way this display can contradict itself.
 *
 * Param:
 * milliseconds - number (what is left, already known to be positive)
 *
 * Return:
 * - mm:ss, with both parts padded to two digits
 */
function labelFor(milliseconds: number): string {
    const totalSeconds = Math.ceil(milliseconds / 1000);

    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;

    return `${pad(minutes)}:${pad(seconds)}`;
}

/** Pads one part of the label to two digits. */
function pad(value: number): string {
    return value.toString().padStart(2, "0");
}
