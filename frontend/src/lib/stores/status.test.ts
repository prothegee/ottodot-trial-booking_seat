import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Readiness, VersionIdentity } from "$lib/api/types";
import { createStatusStore, readinessPath, versionPath } from "$lib/stores/status";

const identity: VersionIdentity = {
    service: "ottodot-trial-booking-api",
    version: "0.1.0",
    commit: "6b30337",
    built_at: "2026-08-11T09:00:00Z",
    runtime: "go1.26.5",
};

const allReady: Readiness = {
    status: "ready",
    checks: { postgres_primary: "ok", postgres_replica: "ok", redis: "ok" },
};

const replicaDown: Readiness = {
    status: "degraded",
    checks: { postgres_primary: "ok", postgres_replica: "down", redis: "ok" },
};

/** A client that answers the two operations routes however a case wants. */
function stubClient(answers: {
    version?: VersionIdentity | Error;
    readiness?: Readiness | Error;
}) {
    const calls: string[] = [];

    return {
        calls,
        client: {
            async request<T>(request: { method: string; path: string }): Promise<T> {
                calls.push(request.path);

                const answer = request.path === versionPath ? answers.version : answers.readiness;

                if (answer instanceof Error) {
                    throw answer;
                }

                return answer as T;
            },
        },
    };
}

describe("the status store", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("integration: opening reads both routes once", async () => {
        const stub = stubClient({ version: identity, readiness: allReady });
        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        expect(stub.calls).toEqual(expect.arrayContaining([versionPath, readinessPath]));
        expect(get(store).version).toEqual(identity);
        expect(get(store).readiness?.status).toBe("ready");

        store.close();
    });

    test("behaviour: it keeps asking while it is open and stops when it is closed", async () => {
        // The only timer in this client. A poll that outlives its screen is a
        // request every fifteen seconds for a page nobody is looking at.
        const stub = stubClient({ version: identity, readiness: allReady });
        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        const afterFirst = stub.calls.length;

        await vi.advanceTimersByTimeAsync(2500);

        const whileOpen = stub.calls.length;

        expect(whileOpen).toBeGreaterThan(afterFirst);

        store.close();

        await vi.advanceTimersByTimeAsync(5000);

        expect(stub.calls.length).toBe(whileOpen);
    });

    test("edge: a 503 carrying a readiness body is an answer, not a failure", async () => {
        // The api answers unready with the report naming which dependency is
        // down. Treating that status as an error would throw away the only
        // information the screen exists to show.
        const unready: Readiness = {
            status: "unavailable",
            checks: { postgres_primary: "down", postgres_replica: "ok", redis: "ok" },
        };

        const stub = stubClient({
            version: identity,
            readiness: new ApiError("Unavailable", "dependency_unavailable", 503, 0, "", "", unready),
        });

        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        expect(get(store).readiness?.status).toBe("unavailable");
        expect(get(store).readiness?.checks.postgres_primary).toBe("down");

        store.close();
    });

    test("edge: degraded is distinct from unavailable", async () => {
        // A replica that is down costs a class list some accuracy and costs
        // nobody a seat, because no seat is decided from it. Colouring that the
        // same as an unready service would send somebody looking for a problem
        // that is not costing anything.
        const stub = stubClient({ version: identity, readiness: replicaDown });
        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        expect(get(store).readiness?.status).toBe("degraded");
        expect(get(store).readiness?.checks.postgres_replica).toBe("down");

        store.close();
    });

    test("edge: a backend that is not talking at all reports a failure", async () => {
        // Separate from an unready body on purpose. One is a service telling the
        // truth about being broken, and the other is a service that is not
        // there.
        const stub = stubClient({
            version: new ApiError("Unavailable", "missing_envelope", 0),
            readiness: allReady,
        });

        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        expect(get(store).failure).not.toBe("");
        expect(get(store).version).toBeNull();

        store.close();
    });

    test("unit: closing twice is harmless", async () => {
        const stub = stubClient({ version: identity, readiness: allReady });
        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();

        store.close();
        store.close();

        expect(get(store).version).toEqual(identity);
    });

    test("unit: resetting empties everything the screen was holding", async () => {
        const stub = stubClient({ version: identity, readiness: allReady });
        const store = createStatusStore({ client: stub.client, intervalMs: 1000 });

        await store.open();
        store.close();
        store.reset();

        expect(get(store)).toEqual({ version: null, readiness: null, loading: false, failure: "" });
    });
});
