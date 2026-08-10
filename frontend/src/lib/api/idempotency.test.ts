import { describe, expect, test, vi } from "vitest";

import { idempotencyKeyHeader, newIdempotencyKey } from "$lib/api/idempotency";

/** Runs one body in an environment where crypto.randomUUID does not exist. */
function withoutRandomUUID(body: () => void): void {
    Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true });

    try {
        body();
    } finally {
        Reflect.deleteProperty(crypto, "randomUUID");
    }
}

describe("the idempotency key", () => {
    test("unit: the header name is the one the api reads", () => {
        // Header names travel lowercase through this client, and the api reads
        // this exact string. A capital letter here would be sent and ignored.
        expect(idempotencyKeyHeader).toBe("idempotency-key");
    });

    test("unit: every call produces a different key", () => {
        const keys = new Set(Array.from({ length: 100 }, () => newIdempotencyKey()));

        expect(keys.size).toBe(100);
    });

    test("unit: a key is long enough to be worth calling unguessable", () => {
        // A short key is a key somebody can walk. The backend honours a
        // matching key as a promise that two calls are the same call, so
        // guessing one is guessing somebody else's attempt.
        expect(newIdempotencyKey().length).toBeGreaterThanOrEqual(32);
    });

    test("edge: an environment without randomUUID still gets a random key", () => {
        // Shadowed with undefined rather than deleted, because the real
        // function lives on the prototype and deleting an own property that is
        // not there would leave the branch under test unreached.
        withoutRandomUUID(() => {
            const first = newIdempotencyKey();
            const second = newIdempotencyKey();

            expect(first).not.toBe(second);
            expect(first).toMatch(/^[0-9a-f]{32}$/);
        });
    });

    test("edge: the fallback is random rather than a clock or a counter", () => {
        // A timestamp or a counter would still be unique and would still pass
        // every case above. It would also be guessable, which is the one thing
        // an idempotency key must not be.
        const randomValues = vi.spyOn(crypto, "getRandomValues");

        try {
            withoutRandomUUID(() => {
                newIdempotencyKey();
            });

            expect(randomValues).toHaveBeenCalled();
        } finally {
            randomValues.mockRestore();
        }
    });
});
