/**
 * Measuring what a form reports about how it was filled in.
 *
 * The shape itself is a wire type and lives in `api/types.ts`. What is here is
 * the measurement, and the one rule that governs it: the backend owns every bot
 * prevention decision, and this side blocks nothing.
 *
 * That rule is worth stating rather than assuming. A client that refused its own
 * submission would teach a script exactly which value to change, and would
 * refuse a slow person on a bad connection for no reason. The measurement is
 * evidence, and evidence is weighed on the other side.
 */
import type { BotSignals } from "$lib/api/types";

/**
 * The mock challenge token this client's widget produces.
 *
 * It matches the value the backend's mock verifier accepts, which is what makes
 * the two halves testable end to end without an account anywhere. A real
 * provider replaces both sides at once.
 */
export const mockCaptchaToken = "mock-captcha-pass";

/**
 * Builds the signals for one submission.
 *
 * Note:
 * - the elapsed time is a real measurement between two instants, never a
 *   constant. A fixed number would pass the backend's check while proving
 *   nothing about the person, which is the same as not sending it.
 * - a clock that moved backwards produces zero rather than a negative. The api
 *   reads zero as "not measured", which is the honest answer when the
 *   measurement cannot be trusted.
 *
 * Param:
 * honeypot - string (whatever is in the hidden field)
 * mountedAt - number (when the form appeared, from performance.now or Date.now)
 * submittedAt - number (now, from the same clock)
 * captchaToken - string (what the widget produced, empty when it has not)
 *
 * Return:
 * - the three values, ready to go in a request body
 */
export function botSignals(
    honeypot: string,
    mountedAt: number,
    submittedAt: number,
    captchaToken: string,
): BotSignals {
    const elapsed = submittedAt - mountedAt;

    return {
        website: honeypot,
        filled_in_ms: elapsed > 0 ? Math.round(elapsed) : 0,
        captcha_token: captchaToken,
    };
}

/** Signals for a submission that measured nothing, which the api accepts. */
export function unmeasuredSignals(): BotSignals {
    return { website: "", filled_in_ms: 0, captcha_token: "" };
}

export type { BotSignals };
