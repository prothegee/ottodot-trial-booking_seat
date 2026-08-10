import { beforeEach, describe, expect, test } from "vitest";

import { createSessionMirror } from "$lib/cache/session_mirror";

interface StoredCount {
    seats_remaining: number;
}

/** A storage that refuses every call, as a full or blocked one does. */
const refusingStorage = {
    // Claims to hold something, so clearing walks it and hits the refusal
    // rather than finding nothing to do.
    length: 1,
    getItem(): string | null {
        throw new Error("storage is not available");
    },
    setItem(): void {
        throw new Error("the quota is full");
    },
    removeItem(): void {
        throw new Error("storage is not available");
    },
    key(): string | null {
        throw new Error("storage is not available");
    },
};

describe("the session mirror", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("unit: a value written can be read back", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        mirror.write("/api/v1/classes", { seats_remaining: 3 });

        expect(mirror.read("/api/v1/classes")).toEqual({ seats_remaining: 3 });
    });

    test("unit: a key never written reads as nothing", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        expect(mirror.read("/api/v1/classes")).toBeNull();
    });

    test("unit: the namespace is part of the stored key", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        mirror.write("/api/v1/classes", { seats_remaining: 1 });

        expect(sessionStorage.getItem("cache:/api/v1/classes")).not.toBeNull();
    });

    test("unit: a removed key is gone", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        mirror.write("/api/v1/classes", { seats_remaining: 1 });
        mirror.remove("/api/v1/classes");

        expect(mirror.read("/api/v1/classes")).toBeNull();
    });

    test("unit: the keys report what this namespace holds, with the prefix stripped", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        mirror.write("/api/v1/classes", { seats_remaining: 1 });
        sessionStorage.setItem("something-else", "not ours");

        expect(mirror.keys()).toEqual(["/api/v1/classes"]);
    });

    test("edge: clearing takes this namespace and nothing else", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        mirror.write("/api/v1/classes", { seats_remaining: 1 });
        sessionStorage.setItem("something-else", "kept");

        mirror.clear();

        expect(mirror.read("/api/v1/classes")).toBeNull();
        expect(sessionStorage.getItem("something-else")).toBe("kept");
    });

    test("edge: a storage that throws is treated as no storage at all", () => {
        // Quota exhaustion, a disabled storage, and private browsing all throw
        // from these calls. None of them is a reason for a page to break.
        const mirror = createSessionMirror<StoredCount>("cache", refusingStorage);

        expect(() => mirror.write("/api/v1/classes", { seats_remaining: 1 })).not.toThrow();
        expect(() => mirror.remove("/api/v1/classes")).not.toThrow();
        expect(() => mirror.clear()).not.toThrow();
        expect(mirror.read("/api/v1/classes")).toBeNull();
    });

    test("edge: an entry that will not parse reads as nothing, so the read falls through", () => {
        const mirror = createSessionMirror<StoredCount>("cache");

        sessionStorage.setItem("cache:/api/v1/classes", "{ half written");

        expect(mirror.read("/api/v1/classes")).toBeNull();
    });

    test("edge: with no storage present nothing throws and nothing is held", () => {
        const mirror = createSessionMirror<StoredCount>("cache", null);

        mirror.write("/api/v1/classes", { seats_remaining: 1 });

        expect(mirror.read("/api/v1/classes")).toBeNull();
    });
});
