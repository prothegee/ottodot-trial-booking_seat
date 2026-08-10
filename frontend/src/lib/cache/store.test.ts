import { beforeEach, describe, expect, test, vi } from "vitest";

import { cacheNamespace, createCacheStore, type CacheEntry } from "$lib/cache/store";
import { createSessionMirror } from "$lib/cache/session_mirror";
import { classListPath } from "$lib/cache/key";
import { tierForAge } from "$lib/cache/policy";

const listBody = { classes: [{ id: "0192a000-0000-7000-8000-000000000021", seats_remaining: 3 }] };

/** A clock a test moves by hand, so no entry has to be waited for. */
function fixedClock(startAt: number) {
    let current = startAt;

    return {
        read: () => current,
        advanceBy: (milliseconds: number) => {
            current += milliseconds;
        },
    };
}

describe("the cache store", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("unit: a saved entry reads back with its body, its tag, and its time", () => {
        const clock = fixedClock(1_000);
        const store = createCacheStore({ now: clock.read });

        store.save(classListPath, listBody, "\"v41\"");

        expect(store.read(classListPath)).toEqual({ body: listBody, etag: "\"v41\"", storedAt: 1_000 });
    });

    test("unit: a key never saved reads as nothing", () => {
        const store = createCacheStore();

        expect(store.read(classListPath)).toBeNull();
    });

    test("unit: the tag is kept verbatim, never parsed or trimmed", () => {
        // Only the api knows what is inside its own tags. A weak validator has
        // to come back exactly as it arrived.
        const store = createCacheStore();

        store.save(classListPath, listBody, "W/\"v41-gzip\"");

        expect(store.read(classListPath)?.etag).toBe("W/\"v41-gzip\"");
    });

    test("unit: saving notifies, because the body changed", () => {
        const store = createCacheStore();
        const notified: string[] = [];

        store.subscribe((key) => notified.push(key));
        store.save(classListPath, listBody, "\"v41\"");

        expect(notified).toEqual([classListPath]);
    });

    test("unit: touching refreshes the age and keeps body and tag", () => {
        const clock = fixedClock(1_000);
        const store = createCacheStore({ now: clock.read });

        store.save(classListPath, listBody, "\"v41\"");
        clock.advanceBy(10_000);
        store.touch(classListPath);

        expect(store.read(classListPath)).toEqual({ body: listBody, etag: "\"v41\"", storedAt: 11_000 });
    });

    test("edge: touching notifies nobody, because nothing changed", () => {
        // This is what keeps a 304 from repainting a screen that is already
        // showing the right list.
        const store = createCacheStore();
        const notified: string[] = [];

        store.save(classListPath, listBody, "\"v41\"");
        store.subscribe((key) => notified.push(key));
        store.touch(classListPath);

        expect(notified).toEqual([]);
    });

    test("edge: touching a key that is not held does nothing and does not create one", () => {
        const store = createCacheStore();

        store.touch(classListPath);

        expect(store.read(classListPath)).toBeNull();
    });

    test("unit: an invalidated entry is cold, and keeps the tag that makes the next read cheap", () => {
        const clock = fixedClock(100_000);
        const store = createCacheStore({ now: clock.read });

        store.save(classListPath, listBody, "\"v41\"");
        store.invalidate(classListPath);

        const held = store.read(classListPath);

        expect(held?.etag).toBe("\"v41\"");
        expect(tierForAge(clock.read() - (held?.storedAt ?? 0))).toBe("cold");
    });

    test("edge: invalidating reaches an entry only the mirror knows about", () => {
        // After a reload the map is empty and the mirror is not. An entry
        // nobody has read yet is exactly the one that would still be believed.
        const mirror = createSessionMirror<CacheEntry>(cacheNamespace);

        mirror.write(classListPath, { body: listBody, etag: "\"v41\"", storedAt: 100_000 });

        const store = createCacheStore({ now: () => 100_000 });

        store.invalidateAll();

        const held = store.read(classListPath);

        expect(held?.etag).toBe("\"v41\"");
        expect(tierForAge(100_000 - (held?.storedAt ?? 0))).toBe("cold");
    });

    test("edge: invalidating a key that is not held creates nothing", () => {
        const store = createCacheStore();

        store.invalidate(classListPath);

        expect(store.read(classListPath)).toBeNull();
    });

    test("unit: clearing drops every entry", () => {
        const store = createCacheStore();

        store.save(classListPath, listBody, "\"v41\"");
        store.save(classListPath + "?subject=maths", listBody, "\"v42\"");
        store.clear();

        expect(store.read(classListPath)).toBeNull();
        expect(store.read(classListPath + "?subject=maths")).toBeNull();
        expect(sessionStorage.length).toBe(0);
    });

    test("integration: an entry survives a reload, and so does its tag", () => {
        // The tag is the part that matters. A body without one costs a full
        // response on the first request after a reload.
        const first = createCacheStore({ now: () => 1_000 });

        first.save(classListPath, listBody, "\"v41\"");

        const afterReload = createCacheStore({ now: () => 90_000 });

        expect(afterReload.read(classListPath)).toEqual({
            body: listBody,
            etag: "\"v41\"",
            storedAt: 1_000,
        });
    });

    test("edge: a storage that refuses to write never reaches the caller", () => {
        // A full quota, a disabled storage, and private browsing all throw
        // from setItem. The cache is an optimisation, so the page carries on
        // with the memory copy and simply does not survive a reload.
        const refusal = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
            throw new Error("the quota is full");
        });

        const store = createCacheStore({ now: () => 1_000 });

        expect(() => store.save(classListPath, listBody, "\"v41\"")).not.toThrow();
        expect(store.read(classListPath)?.body).toEqual(listBody);

        refusal.mockRestore();
    });

    test("edge: a stored entry read straight from the mirror keeps its stored time", () => {
        const mirror = createSessionMirror<CacheEntry>(cacheNamespace);

        mirror.write(classListPath, { body: listBody, etag: "\"v41\"", storedAt: 42 });

        const store = createCacheStore({ now: () => 90_000 });

        expect(store.read(classListPath)?.storedAt).toBe(42);
    });

    test("unit: a listener that unsubscribes stops being told", () => {
        const store = createCacheStore();
        const notified: string[] = [];

        const stop = store.subscribe((key) => notified.push(key));

        store.save(classListPath, listBody, "\"v41\"");
        stop();
        store.save(classListPath, listBody, "\"v42\"");

        expect(notified).toEqual([classListPath]);
    });
});
