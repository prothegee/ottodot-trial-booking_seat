/**
 * Where an idempotency key comes from.
 *
 * One key covers one attempt by the parent, from the moment they submit a
 * booking until that attempt reaches an answer. Both calls in an attempt carry
 * it, so a retry of either one produces the first answer instead of a second
 * charge.
 *
 * A new attempt gets a new key. A decline is a finished attempt, so trying
 * again is a new one, and reusing the key there would replay the decline
 * forever. That rule lives with the payment screen in phase 5, which calls
 * this file rather than inventing a key of its own.
 *
 * Phase 6 moves the lifecycle into the api client, where a header can be
 * attached without every caller remembering. The minting stays here.
 */

/** The header the api reads the key from. */
export const idempotencyKeyHeader = "idempotency-key";

/** How many random hex characters the fallback produces. */
const fallbackKeyLength = 32;

/**
 * Mints a key for one attempt.
 *
 * Note:
 * - it must be unguessable rather than merely unique. A predictable key would
 *   let one parent's retry collide with another's attempt, which the backend
 *   would honour, because a key is a promise that two calls are the same call.
 * - `crypto.randomUUID` is the source wherever it exists. The fallback is for
 *   an environment without it, and it uses the same random source rather than
 *   a timestamp or a counter, both of which are guessable.
 *
 * Return:
 * - a key, different on every call
 */
export function newIdempotencyKey(): string {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }

    const bytes = new Uint8Array(fallbackKeyLength / 2);

    crypto.getRandomValues(bytes);

    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}
