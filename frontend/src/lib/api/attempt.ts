/**
 * The lifecycle of one idempotency key.
 *
 * Minting a key is one line and lives in `idempotency.ts`. Knowing when a key is
 * spent is the part that is easy to get wrong, and it is here, on its own, so it
 * can be read and tested without a store or a screen in front of it.
 *
 * The rule has two halves that fail in opposite directions, which is why it is
 * worth a file rather than an `if`:
 *
 * A decline is a finished attempt. The provider looked at it and said no, and no
 * money moved. Paying again is a new attempt and needs a new key, because
 * sending the old one back replays the decline for as long as the parent keeps
 * trying.
 *
 * A call that broke without an answer is the opposite. Nobody knows whether the
 * charge went through, so the same key has to go back, or the retry risks
 * charging twice.
 */
import { newIdempotencyKey } from "$lib/api/idempotency";

/**
 * The failure kinds that end the attempt they were sent under.
 *
 * `PaymentDeclined` is a finished attempt. `InvalidRequest` is a submission the
 * api would refuse identically every time it arrived, so retrying it under the
 * same key is a request that can never succeed.
 *
 * Everything else keeps the key. `Unavailable` in particular, which is the whole
 * reason this set is a closed list rather than a "was it a failure" check.
 *
 * `SeatLost` and `ClassFull` are in neither camp because there is no retry to
 * make: both are terminal for that class, and the screen offers no way forward.
 */
export const kindsThatEndTheAttempt: ReadonlySet<string> = new Set([
    "PaymentDeclined",
    "InvalidRequest",
]);

/** One attempt's key, and the rule for when it is spent. */
export interface AttemptKey {
    /** The key in force. It mints one on first use rather than answering empty. */
    current(): string;

    /** Begins a new attempt and returns its key. */
    restart(): string;

    /**
     * Reports a failure and decides what it did to the key.
     *
     * Return:
     * - true when the attempt ended and the next call will carry a new key
     * - false when the key still stands, so a retry is a retry
     */
    settle(kind: string): boolean;

    /** Forgets the attempt, which a sign out does. */
    clear(): void;
}

/**
 * Builds a keeper.
 *
 * Param:
 * newKey - () => string (the source, handed in only so a test can pin it)
 *
 * Return:
 * - the keeper, holding no key until one is asked for
 */
export function createAttemptKey(newKey: () => string = newIdempotencyKey): AttemptKey {
    let held = "";

    return {
        current(): string {
            if (held === "") {
                held = newKey();
            }

            return held;
        },

        restart(): string {
            held = newKey();

            return held;
        },

        settle(kind: string): boolean {
            if (!kindsThatEndTheAttempt.has(kind)) {
                return false;
            }

            held = newKey();

            return true;
        },

        clear(): void {
            held = "";
        },
    };
}
