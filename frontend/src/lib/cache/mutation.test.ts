import { beforeEach, describe, expect, test } from "vitest";

import { createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody, type FakeHandler } from "$lib/api/transport_fake";
import { classListPath } from "$lib/cache/key";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { tierForAge } from "$lib/cache/policy";
import { createCacheStore, type CacheStore } from "$lib/cache/store";

const bookingsPath = "/api/v1/bookings";
const listBody = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 1 }] };

/** A clock well past the stale window, so an aged entry cannot read as fresh. */
const clockNow = () => 100_000;

/** Builds a mutator over a fake transport and a store holding a class list. */
function mutatorOver(handler: FakeHandler) {
    const transport = createFakeTransport(handler);
    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore({ now: clockNow });

    store.save(classListPath, listBody, "\"v41\"");

    return { mutator: createCacheAwareMutator({ client, store }), store, transport };
}

/** What the stored class list would be treated as if it were read right now. */
function ageOf(store: CacheStore) {
    const held = store.read(classListPath);

    if (held === null) {
        return "gone";
    }

    return tierForAge(clockNow() - held.storedAt);
}

describe("a mutation that owns its invalidation", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("integration: a successful mutation leaves no entry that can still be rendered", async () => {
        const { mutator, store } = mutatorOver(() => ({ status: 201, body: { status: "pending_payment" } }));

        await mutator.send({ method: "POST", path: bookingsPath, body: { class_id: "a class" } });

        expect(ageOf(store)).toBe("cold");
    });

    test("integration: the tag survives, so the next read can still be answered with a 304", async () => {
        const { mutator, store } = mutatorOver(() => ({ status: 201, body: { status: "pending_payment" } }));

        await mutator.send({ method: "POST", path: bookingsPath });

        expect(store.read(classListPath)?.etag).toBe("\"v41\"");
    });

    test("integration: the invalidation happens before the caller is answered", async () => {
        // A screen that returns to the list on success must never be served
        // the count it just changed.
        const { mutator, store } = mutatorOver(() => ({ status: 201, body: { status: "pending_payment" } }));

        const sending = mutator.send({ method: "POST", path: bookingsPath }).then(() => ageOf(store));

        await expect(sending).resolves.toBe("cold");
    });

    test("edge: a refused mutation leaves the cache alone", async () => {
        // A rejection is not a change. The class list is as true as it was.
        const { mutator, store } = mutatorOver(() => ({ status: 409, body: errorBody("already_booked") }));

        await expect(mutator.send({ method: "POST", path: bookingsPath })).rejects.toMatchObject({
            kind: "AlreadyBooked",
        });

        expect(store.read(classListPath)?.body).toEqual(listBody);
        expect(ageOf(store)).toBe("fresh");
    });

    test("edge: an unknown outcome leaves the cache alone, because nothing is known to have changed", async () => {
        // A 500 says nothing about the seat or the money. Dropping the entry
        // would claim knowledge this client does not have.
        const { mutator, store } = mutatorOver(() => ({ status: 500, body: errorBody("internal_error") }));

        await expect(mutator.send({ method: "POST", path: bookingsPath })).rejects.toMatchObject({
            kind: "Unavailable",
        });

        expect(store.read(classListPath)?.body).toEqual(listBody);
        expect(ageOf(store)).toBe("fresh");
    });

    test("unit: the answer reaches the caller unchanged", async () => {
        const { mutator } = mutatorOver(() => ({ status: 201, body: { booking_id: "0192a000", status: "pending_payment" } }));

        const created = await mutator.send<{ booking_id: string }>({ method: "POST", path: bookingsPath });

        expect(created.booking_id).toBe("0192a000");
    });
});
