import { beforeEach, describe, expect, test } from "vitest";

import { createApiClient, ifNoneMatchHeader } from "$lib/api/client";
import { createFakeTransport, errorBody, type FakeHandler } from "$lib/api/transport_fake";
import { classListPath } from "$lib/cache/key";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";

const rosterPath = "/api/v1/classes/0192a000-0000-7000-8000-000000000021/roster";

const firstList = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 3 }] };
const secondList = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 0 }] };

/** A clock a test moves by hand, so no age has to be waited for. */
function fixedClock(startAt: number) {
    let current = startAt;

    return {
        read: () => current,
        advanceBy: (milliseconds: number) => {
            current += milliseconds;
        },
    };
}

/** Builds a reader over a fake transport, a fresh store, and a fake clock. */
function readerOver(handler: FakeHandler, clock = fixedClock(1_000)) {
    const transport = createFakeTransport(handler);
    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore({ now: clock.read });
    const reader = createCachedReader({ client, store, now: clock.read });

    return { reader, store, transport, clock };
}

/** Answers every class list request with a body and a tag. */
const answersWithFirstList: FakeHandler = () => ({
    status: 200,
    body: firstList,
    headers: { etag: "\"v41\"" },
});

describe("reading through the cache", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("integration: an empty cache is a miss that fetches and stores", async () => {
        const { reader, store, transport } = readerOver(answersWithFirstList);

        const answer = await reader.read(classListPath);

        expect(answer.result).toBe("miss");
        expect(answer.body).toEqual(firstList);
        expect(transport.calls).toHaveLength(1);
        expect(transport.calls[0].headers?.[ifNoneMatchHeader]).toBeUndefined();
        expect(store.read(classListPath)?.etag).toBe("\"v41\"");
    });

    test("integration: a fresh entry is served from memory and sends nothing", async () => {
        const { reader, transport, clock } = readerOver(answersWithFirstList);

        await reader.read(classListPath);
        clock.advanceBy(4_999);
        const answer = await reader.read(classListPath);

        expect(answer.result).toBe("fresh");
        expect(answer.body).toEqual(firstList);
        expect(answer.revalidation).toBeNull();
        expect(transport.calls).toHaveLength(1);
    });

    test("integration: a stale entry answers at once and revalidates behind it", async () => {
        const { reader, store, transport, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 304 };
        });

        await reader.read(classListPath);
        clock.advanceBy(10_000);
        const answer = await reader.read(classListPath);

        expect(answer.result).toBe("stale");
        expect(answer.body).toEqual(firstList);

        // The caller was answered without waiting for this. Simulation F13
        // holds the response open to prove that ordering.
        await answer.revalidation;

        expect(transport.calls).toHaveLength(2);
        expect(transport.calls[1].headers?.[ifNoneMatchHeader]).toBe("\"v41\"");
        expect(store.read(classListPath)?.storedAt).toBe(11_000);
    });

    test("integration: a cold entry blocks, and a 304 keeps the body it already had", async () => {
        const { reader, store, transport, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 304 };
        });

        await reader.read(classListPath);
        clock.advanceBy(30_000);
        const answer = await reader.read(classListPath);

        expect(answer.result).toBe("revalidated");
        expect(answer.body).toEqual(firstList);
        expect(transport.calls[1].headers?.[ifNoneMatchHeader]).toBe("\"v41\"");
        expect(store.read(classListPath)?.storedAt).toBe(31_000);
    });

    test("integration: a 200 on a cold read replaces body and tag together and notifies", async () => {
        const { reader, store, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 200, body: secondList, headers: { etag: "\"v42\"" } };
        });

        const notified: string[] = [];

        await reader.read(classListPath);
        store.subscribe((key) => notified.push(key));
        clock.advanceBy(31_000);
        const answer = await reader.read(classListPath);

        expect(answer.result).toBe("miss");
        expect(answer.body).toEqual(secondList);
        expect(store.read(classListPath)?.etag).toBe("\"v42\"");
        expect(notified).toEqual([classListPath]);
    });

    test("edge: a 304 never notifies, so a screen showing the right list is not repainted", async () => {
        const { reader, store, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 304 };
        });

        const notified: string[] = [];

        await reader.read(classListPath);
        store.subscribe((key) => notified.push(key));
        clock.advanceBy(30_000);
        await reader.read(classListPath);

        expect(notified).toEqual([]);
    });

    test("edge: a path this cache may not hold is fetched and never stored", async () => {
        // Routing a roster through the reader must not turn it into an entry.
        // It names real children and it is never advisory.
        const { reader, store, transport } = readerOver(() => ({
            status: 200,
            body: { bookings: [] },
            headers: { etag: "\"r7\"" },
        }));

        const first = await reader.read(rosterPath);
        const second = await reader.read(rosterPath);

        expect(first.result).toBe("miss");
        expect(second.result).toBe("miss");
        expect(store.read(rosterPath)).toBeNull();
        expect(transport.calls).toHaveLength(2);
    });

    test("edge: a background revalidation that fails leaves the stale body on screen", async () => {
        const { reader, store, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 503, body: errorBody("dependency_unavailable") };
        });

        await reader.read(classListPath);
        clock.advanceBy(10_000);
        const answer = await reader.read(classListPath);

        await expect(answer.revalidation).resolves.toBeUndefined();

        expect(answer.body).toEqual(firstList);
        expect(store.read(classListPath)?.body).toEqual(firstList);
    });

    test("edge: two stale reads share one revalidation", async () => {
        // The second call would buy nothing, for the same reason the token
        // refresh is single flight.
        const { reader, transport, clock } = readerOver((request, callIndex) => {
            if (callIndex === 0) {
                return { status: 200, body: firstList, headers: { etag: "\"v41\"" } };
            }

            return { status: 304 };
        });

        await reader.read(classListPath);
        clock.advanceBy(10_000);

        const [first, second] = await Promise.all([reader.read(classListPath), reader.read(classListPath)]);

        await Promise.all([first.revalidation, second.revalidation]);

        expect(transport.calls).toHaveLength(2);
    });

    test("edge: a cold read fails loudly when the api says nothing changed and nothing is held", async () => {
        // Only a broken api can answer a question that was never asked.
        // Guessing a body would be worse than saying so.
        const { reader } = readerOver(() => ({ status: 304 }));

        await expect(reader.read(classListPath)).rejects.toMatchObject({
            kind: "Unavailable",
            code: "not_modified_without_copy",
        });
    });

    test("integration: an entry mirrored by an earlier page load is used and revalidated", async () => {
        const clock = fixedClock(1_000);
        const first = readerOver(answersWithFirstList, clock);

        await first.reader.read(classListPath);

        // A reload: new store, new reader, same session storage.
        clock.advanceBy(60_000);

        const afterReload = readerOver(() => ({ status: 304 }), clock);
        const answer = await afterReload.reader.read(classListPath);

        expect(answer.result).toBe("revalidated");
        expect(answer.body).toEqual(firstList);
        expect(afterReload.transport.calls[0].headers?.[ifNoneMatchHeader]).toBe("\"v41\"");
    });
});
