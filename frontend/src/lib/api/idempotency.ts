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
 * forever. That rule is in `attempt.ts`, next to this file, so it can be read
 * without a store or a screen around it. The minting stays here, and so does
 * the header, so no caller writes the header name for itself.
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

/**
 * Puts a key on a request's headers.
 *
 * It exists so no caller writes the header name for itself. A route that spelt
 * it differently would send a key the api never reads, and the failure would be
 * a second charge rather than an error anybody sees.
 *
 * Param:
 * key - string (the key for the attempt in force)
 * headers - Record<string, string> (anything the caller already wanted to send)
 *
 * Return:
 * - the headers, with the key on them
 */
export function withIdempotencyKey(
    key: string,
    headers: Record<string, string> = {},
): Record<string, string> {
    return { ...headers, [idempotencyKeyHeader]: key };
}
