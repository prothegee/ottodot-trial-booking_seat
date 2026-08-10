import { describe, expect, test } from "vitest";

import { cacheKeyFor, classListPath, isCacheableClassPath } from "$lib/cache/key";

const singleClassPath = classListPath + "/0192a000-0000-7000-8000-000000000021";

describe("what the cache may hold", () => {
    test("unit: the class list and a single class are cacheable", () => {
        expect(isCacheableClassPath(classListPath)).toBe(true);
        expect(isCacheableClassPath(singleClassPath)).toBe(true);
    });

    test("unit: nothing that decides or names a child is cacheable", () => {
        // A booking decides. A roster names real children. Neither is
        // advisory, so a stale copy of either is a wrong answer.
        expect(isCacheableClassPath("/api/v1/bookings")).toBe(false);
        expect(isCacheableClassPath("/api/v1/auth/me")).toBe(false);
        expect(isCacheableClassPath(singleClassPath + "/roster")).toBe(false);
    });

    test("edge: a path that only starts like the class list is not cacheable", () => {
        expect(isCacheableClassPath("/api/v1/classesroom")).toBe(false);
        expect(isCacheableClassPath(classListPath + "/")).toBe(false);
    });

    test("unit: a cacheable GET gets its path as the key", () => {
        expect(cacheKeyFor("GET", classListPath)).toBe(classListPath);
        expect(cacheKeyFor("GET", singleClassPath)).toBe(singleClassPath);
    });

    test("unit: a mutation never has a key", () => {
        expect(cacheKeyFor("POST", classListPath)).toBeNull();
        expect(cacheKeyFor("DELETE", singleClassPath)).toBeNull();
    });

    test("edge: two different query strings never collide", () => {
        // The failure this guards against is a filtered list being served for
        // an unfiltered one, which would hide classes a parent could book.
        const filtered = cacheKeyFor("GET", classListPath + "?subject=maths");
        const other = cacheKeyFor("GET", classListPath + "?subject=science");
        const unfiltered = cacheKeyFor("GET", classListPath);

        expect(filtered).not.toBe(other);
        expect(filtered).not.toBe(unfiltered);
        expect(other).not.toBe(unfiltered);
    });

    test("edge: a query string keeps a path cacheable", () => {
        expect(isCacheableClassPath(classListPath + "?subject=maths")).toBe(true);
        expect(cacheKeyFor("GET", classListPath + "?subject=maths")).toContain("?subject=maths");
    });

    test("edge: the method is read case insensitively", () => {
        expect(cacheKeyFor("get", classListPath)).toBe(classListPath);
        expect(cacheKeyFor("post", classListPath)).toBeNull();
    });
});
