import { beforeEach, describe, expect, test } from "vitest";

import { createApiClient, ifNoneMatchHeader } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import { classListPath } from "$lib/cache/key";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";

/**
 * Simulation F14: a mutation invalidates the cache.
 *
 *     payment confirmed -> the class list entry goes cold
 *     the parent returns to the list -> a blocking GET with If-None-Match
 *     the api answers 200 with a new tag and the updated seat counts
 *
 * Asserts: the entry stops being usable at the moment the mutation succeeds,
 * the next read is not served from memory, the request carries the stored tag,
 * and the new seat count is what the parent sees.
 */

const paymentPath = "/api/v1/bookings/0192a000-0000-7000-8000-000000000031/payment";

const beforePayment = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 1 }] };
const afterPayment = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 0 }] };

/** Builds the reader, the mutator, and the store they share, on one clock. */
function clientSide(transport: ReturnType<typeof createFakeTransport>, now: () => number) {
    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore({ now });

    return {
        store,
        reader: createCachedReader({ client, store, now }),
        mutator: createCacheAwareMutator({ client, store }),
    };
}

describe("simulation F14: a mutation invalidates the cache", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the seat count after a confirmed payment is the api's, not the cached one", async () => {
        const currentTime = 1_000;

        const transport = createFakeTransport((request) => {
            if (request.method === "POST") {
                return { status: 200, body: { status: "confirmed", seat_number: 8 } };
            }

            // The first list is what the parent saw before paying, the second
            // is what the api holds afterwards.
            const seen = request.headers?.[ifNoneMatchHeader] !== undefined;

            return seen
                ? { status: 200, body: afterPayment, headers: { etag: "\"v42\"" } }
                : { status: 200, body: beforePayment, headers: { etag: "\"v41\"" } };
        });

        const { store, reader, mutator } = clientSide(transport, () => currentTime);

        const beforeBooking = await reader.read<typeof beforePayment>(classListPath);

        expect(beforeBooking.body.classes[0].seats_remaining).toBe(1);

        await mutator.send({ method: "POST", path: paymentPath });

        // The clock has not moved. Without the invalidation this read would be
        // fresh, and the parent would be shown the seat they just took.
        const afterBooking = await reader.read<typeof afterPayment>(classListPath);

        expect(afterBooking.result).not.toBe("fresh");
        expect(afterBooking.result).not.toBe("stale");
        expect(afterBooking.body.classes[0].seats_remaining).toBe(0);

        expect(transport.calls).toHaveLength(3);
        expect(transport.calls[2].headers?.[ifNoneMatchHeader]).toBe("\"v41\"");
        expect(store.read(classListPath)?.etag).toBe("\"v42\"");
    });

    test("behaviour: a mutation that failed changes nothing, because nothing is known to have changed", async () => {
        // The client-side half of the unknown outcome. A 500 says nothing
        // about the seat or the money, so claiming the list moved would be a
        // guess.
        const currentTime = 1_000;

        const transport = createFakeTransport((request) => {
            if (request.method === "POST") {
                return { status: 500, body: errorBody("internal_error") };
            }

            return { status: 200, body: beforePayment, headers: { etag: "\"v41\"" } };
        });

        const { reader, mutator } = clientSide(transport, () => currentTime);

        await reader.read(classListPath);

        await expect(mutator.send({ method: "POST", path: paymentPath })).rejects.toMatchObject({
            kind: "Unavailable",
        });

        const afterFailure = await reader.read(classListPath);

        expect(afterFailure.result).toBe("fresh");
        expect(transport.calls).toHaveLength(2);
    });
});
