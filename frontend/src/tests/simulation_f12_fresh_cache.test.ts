import { beforeEach, describe, expect, test } from "vitest";

import { createApiClient } from "$lib/api/client";
import { createFakeTransport } from "$lib/api/transport_fake";
import { classListPath } from "$lib/cache/key";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";

/**
 * Simulation F12: a fresh cache sends no request at all.
 *
 *     parent -> client: open the class list
 *     client -> transport: GET classes
 *     transport -> client: 200 with a body and an ETag
 *     parent -> client: navigate away and back within five seconds
 *     note over client: served from memory
 *     note over transport: no second call is recorded
 *
 * Asserts: the fake transport records exactly one call, the rendered list is
 * identical, and the lookup reports itself as fresh.
 *
 * The plan lists a queued telemetry event here. The emitter is phase 7, so the
 * assert is on the returned lookup result, which is the value that event will
 * be built from.
 */

const classList = {
    classes: [
        { id: "0192a000-0000-7000-8000-000000000021", subject: "Maths", seats_remaining: 3 },
        { id: "0192a000-0000-7000-8000-000000000022", subject: "Science", seats_remaining: 1 },
    ],
};

describe("simulation F12: a fresh cache sends no request", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: opening the list twice within five seconds costs one call", async () => {
        let currentTime = 1_000;

        const transport = createFakeTransport(() => ({
            status: 200,
            body: classList,
            headers: { etag: "\"v41\"" },
        }));

        const client = createApiClient({ transport, onSignOut: () => {} });
        const store = createCacheStore({ now: () => currentTime });
        const reader = createCachedReader({ client, store, now: () => currentTime });

        const firstView = await reader.read<typeof classList>(classListPath);

        // The parent opens a class, thinks better of it, and comes back.
        currentTime += 4_000;

        const secondView = await reader.read<typeof classList>(classListPath);

        expect(firstView.result).toBe("miss");
        expect(secondView.result).toBe("fresh");

        // The whole point of the simulation. Not one call answered from cache,
        // one call in total, because the second view never reached the api.
        expect(transport.calls).toHaveLength(1);

        expect(secondView.body).toEqual(firstView.body);
        expect(secondView.body.classes[1].seats_remaining).toBe(1);
    });

    test("behaviour: a fresh view starts no background work either", async () => {
        // A revalidation nobody waits for is still a request the backend has
        // to answer. Fresh means nothing is sent, not that nothing is awaited.
        const transport = createFakeTransport(() => ({
            status: 200,
            body: classList,
            headers: { etag: "\"v41\"" },
        }));

        const client = createApiClient({ transport, onSignOut: () => {} });
        const store = createCacheStore({ now: () => 1_000 });
        const reader = createCachedReader({ client, store, now: () => 1_000 });

        await reader.read(classListPath);
        const secondView = await reader.read(classListPath);

        expect(secondView.revalidation).toBeNull();

        await Promise.resolve();

        expect(transport.calls).toHaveLength(1);
    });
});
