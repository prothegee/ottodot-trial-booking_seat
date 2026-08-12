import { beforeEach, describe, expect, test } from "vitest";

import { createApiClient, ifNoneMatchHeader } from "$lib/api/client";
import { createFakeTransport } from "$lib/api/transport_fake";
import { classListPath } from "$lib/cache/key";
import { tierForAge } from "$lib/cache/policy";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";

/**
 * Test 13: a stale cache revalidates to a 304.
 *
 *     note over client: the entry is ten seconds old
 *     parent -> client: open the class list
 *     client -> parent: the stale list renders immediately
 *     client -> transport: GET classes, If-None-Match "v41"
 *     transport -> client: 304 Not Modified
 *     note over client: body kept, time refreshed, nothing repainted
 *
 * Asserts: the stale body is returned before the request resolves,
 * If-None-Match carries the stored tag, the 304 does not replace the body,
 * subscribers are not needlessly notified, and the entry is fresh again
 * afterwards.
 */

const classList = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 3 }] };

/** A promise a test resolves by hand, so a request can be held mid-flight. */
function heldAnswer() {
    let release = () => {};

    const held = new Promise<void>((resolve) => {
        release = resolve;
    });

    return { held, release: () => release() };
}

describe("test 13: a stale cache revalidates to a 304", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the stale list is on screen before the api has answered", async () => {
        let currentTime = 1_000;
        const revalidation = heldAnswer();

        const transport = createFakeTransport(async (request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: classList, headers: { etag: "\"v41\"" } };
            }

            // The second call hangs until the test lets it finish, which is
            // how the stale render is proven to happen first.
            await revalidation.held;

            return { status: 304 };
        });

        const client = createApiClient({ transport, onSignOut: () => {} });
        const store = createCacheStore({ now: () => currentTime });
        const reader = createCachedReader({ client, store, now: () => currentTime });

        await reader.read(classListPath);

        currentTime += 10_000;

        const notified: string[] = [];
        store.subscribe((key) => notified.push(key));

        const staleView = await reader.read<typeof classList>(classListPath);

        // The api has not answered yet, and the parent already has the list.
        expect(staleView.result).toBe("stale");
        expect(staleView.body).toEqual(classList);
        expect(tierForAge(currentTime - (store.read(classListPath)?.storedAt ?? 0))).toBe("stale");

        revalidation.release();
        await staleView.revalidation;

        const conditional = transport.calls[1];

        expect(conditional.method).toBe("GET");
        expect(conditional.headers?.[ifNoneMatchHeader]).toBe("\"v41\"");

        // A 304 changes nothing a subscriber has to hear about.
        expect(notified).toEqual([]);

        const afterwards = store.read(classListPath);

        expect(afterwards?.body).toEqual(classList);
        expect(afterwards?.etag).toBe("\"v41\"");
        expect(tierForAge(currentTime - (afterwards?.storedAt ?? 0))).toBe("fresh");
    });

    test("behaviour: the next view after the revalidation sends nothing at all", async () => {
        // The point of refreshing the time on a 304: the entry is fresh again,
        // so the following view is free.
        let currentTime = 1_000;

        const transport = createFakeTransport((request, callIndex) =>
            callIndex === 0
                ? { status: 200, body: classList, headers: { etag: "\"v41\"" } }
                : { status: 304 },
        );

        const client = createApiClient({ transport, onSignOut: () => {} });
        const store = createCacheStore({ now: () => currentTime });
        const reader = createCachedReader({ client, store, now: () => currentTime });

        await reader.read(classListPath);

        currentTime += 10_000;

        const staleView = await reader.read(classListPath);
        await staleView.revalidation;

        const nextView = await reader.read(classListPath);

        expect(nextView.result).toBe("fresh");
        expect(transport.calls).toHaveLength(2);
    });
});
