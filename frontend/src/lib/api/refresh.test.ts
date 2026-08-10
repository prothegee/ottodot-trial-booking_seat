import { describe, expect, test } from "vitest";

import { createRefreshCoordinator } from "$lib/api/refresh";

/** A promise a test can settle by hand, so timing is decided rather than raced. */
function deferred<T>() {
    let resolve: (value: T) => void = () => {};
    let reject: (reason: unknown) => void = () => {};

    const promise = new Promise<T>((resolveWith, rejectWith) => {
        resolve = resolveWith;
        reject = rejectWith;
    });

    return { promise, resolve, reject };
}

describe("the refresh coordinator", () => {
    test("unit: one refresh call is issued no matter how many callers are waiting", async () => {
        // Five refreshes would rotate the refresh token five times, and the
        // backend treats a second use of a rotated token as reuse. So this is
        // not an optimisation, it is what keeps the session alive.
        const gate = deferred<void>();
        let refreshes = 0;

        const coordinator = createRefreshCoordinator({
            refresh: () => {
                refreshes += 1;

                return gate.promise;
            },
            onFailure: () => {},
        });

        const waiting = [coordinator.run(), coordinator.run(), coordinator.run(), coordinator.run(), coordinator.run()];

        gate.resolve();
        await Promise.all(waiting);

        expect(refreshes).toBe(1);
    });

    test("unit: every waiting caller resolves once the single refresh succeeds", async () => {
        const gate = deferred<void>();
        const settled: number[] = [];

        const coordinator = createRefreshCoordinator({
            refresh: () => gate.promise,
            onFailure: () => {},
        });

        const waiting = [0, 1, 2].map((index) => coordinator.run().then(() => settled.push(index)));

        gate.resolve();
        await Promise.all(waiting);

        expect(settled).toHaveLength(3);
    });

    test("edge: a failed refresh reports once, not once per waiting caller", async () => {
        const gate = deferred<void>();
        let signOuts = 0;

        const coordinator = createRefreshCoordinator({
            refresh: () => gate.promise,
            onFailure: () => {
                signOuts += 1;
            },
        });

        const waiting = [coordinator.run(), coordinator.run(), coordinator.run()];

        gate.reject(new Error("refused"));
        await Promise.allSettled(waiting);

        expect(signOuts).toBe(1);
    });

    test("edge: a failed refresh rejects every waiting caller", async () => {
        const coordinator = createRefreshCoordinator({
            refresh: () => Promise.reject(new Error("refused")),
            onFailure: () => {},
        });

        const outcomes = await Promise.allSettled([coordinator.run(), coordinator.run()]);

        expect(outcomes.every((outcome) => outcome.status === "rejected")).toBe(true);
    });

    test("edge: a later 401 starts a fresh attempt rather than reusing the last one", async () => {
        // The in-flight promise has to be cleared once it settles. Keeping it
        // would mean one expiry an hour ago permanently poisons every call
        // that follows.
        let refreshes = 0;

        const coordinator = createRefreshCoordinator({
            refresh: () => {
                refreshes += 1;

                return Promise.resolve();
            },
            onFailure: () => {},
        });

        await coordinator.run();
        await coordinator.run();

        expect(refreshes).toBe(2);
    });

    test("edge: a refresh that throws synchronously is still reported, not raised", async () => {
        let reported = 0;

        const coordinator = createRefreshCoordinator({
            refresh: () => {
                throw new Error("no transport");
            },
            onFailure: () => {
                reported += 1;
            },
        });

        await expect(coordinator.run()).rejects.toThrow("no transport");
        expect(reported).toBe(1);
    });
});
