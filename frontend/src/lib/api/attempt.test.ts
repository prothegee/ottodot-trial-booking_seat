import { describe, expect, test } from "vitest";

import { createAttemptKey, kindsThatEndTheAttempt } from "./attempt";

/** A key source a test can read, so an assertion names a value rather than a shape. */
function countingKeys() {
    let minted = 0;

    return () => {
        minted += 1;

        return `key-${minted}`;
    };
}

describe("the attempt key", () => {
    test("unit: the first call mints a key rather than answering empty", () => {
        const attempt = createAttemptKey(countingKeys());

        expect(attempt.current()).toBe("key-1");
    });

    test("unit: the key stands until something spends it", () => {
        const attempt = createAttemptKey(countingKeys());

        expect(attempt.current()).toBe("key-1");
        expect(attempt.current()).toBe("key-1");
        expect(attempt.current()).toBe("key-1");
    });

    test("unit: restarting begins a new attempt", () => {
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.restart()).toBe("key-2");
        expect(attempt.current()).toBe("key-2");
    });

    test("behaviour: a decline ends the attempt, so trying again is a new one", () => {
        // Reusing the key here would replay the decline for as long as the
        // parent kept trying, because the api answers a repeated key with the
        // first answer.
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.settle("PaymentDeclined")).toBe(true);
        expect(attempt.current()).toBe("key-2");
    });

    test("behaviour: a call that broke keeps its key, so a retry is a retry", () => {
        // The opposite failure, and the reason the rule is a closed list rather
        // than a "was it a failure" check. Nobody knows whether the charge went
        // through, so the same key has to go back or the retry risks charging
        // twice.
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.settle("Unavailable")).toBe(false);
        expect(attempt.current()).toBe("key-1");
    });

    test("behaviour: a lost seat keeps its key, because there is no retry to make", () => {
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.settle("SeatLost")).toBe(false);
        expect(attempt.current()).toBe("key-1");
    });

    test("edge: a full class keeps its key for the same reason", () => {
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.settle("ClassFull")).toBe(false);
        expect(attempt.current()).toBe("key-1");
    });

    test("edge: a kind this client has never heard of keeps the key", () => {
        // A backend that grows a code this build does not know must not be able
        // to make a retry charge twice. Keeping the key is the safe default.
        const attempt = createAttemptKey(countingKeys());

        attempt.current();

        expect(attempt.settle("SomethingNewEntirely")).toBe(false);
        expect(attempt.current()).toBe("key-1");
    });

    test("edge: clearing forgets the attempt, which a sign out does", () => {
        const attempt = createAttemptKey(countingKeys());

        attempt.current();
        attempt.clear();

        expect(attempt.current()).toBe("key-2");
    });

    test("unit: the rule is exactly two kinds, so a reader can check it against the api", () => {
        expect([...kindsThatEndTheAttempt].sort()).toEqual(["InvalidRequest", "PaymentDeclined"]);
    });
});
