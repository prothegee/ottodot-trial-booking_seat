/**
 * What one trial class costs.
 *
 * It is a constant here because it is a constant on the backend too: there is
 * no price column on a trial class, and `payment.DefaultCurrency` is fixed for
 * the same reason, one trial class in one country. The api validates the amount
 * it is sent, so this is what the client offers to pay rather than what the
 * charge is decided from.
 *
 * The day a class carries its own price, this file is what a class field
 * replaces, and the screens above it do not change.
 */
import type { BotSignals, PayRequest } from "$lib/api/types";
import { unmeasuredSignals } from "$lib/booking/bot_signals";

/**
 * The charge in the smallest unit of the currency.
 *
 * Cents rather than a decimal, matching the column and the api, because money
 * in a floating point number is how a cent goes missing.
 */
export const trialPriceCents = 4500;

/** The three letter code, matching payment.DefaultCurrency on the backend. */
export const trialCurrency = "SGD";

/**
 * The body one payment is sent with.
 *
 * The signals travel with the amount rather than in a second field, because they
 * describe the submission that produced this charge. A caller with nothing
 * measured gets the unmeasured set, which the api reads as "no evidence" instead
 * of as evidence against.
 *
 * Param:
 * signals - BotSignals (what the form measured, omitted when there was no form)
 *
 * Return:
 * - the request body
 */
export function trialPayment(signals: BotSignals = unmeasuredSignals()): PayRequest {
    return { amount_cents: trialPriceCents, currency: trialCurrency, ...signals };
}
