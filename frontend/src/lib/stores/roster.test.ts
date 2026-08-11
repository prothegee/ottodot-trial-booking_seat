import { get } from "svelte/store";
import { describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Roster } from "$lib/api/types";
import { createRosterStore, rosterPath } from "$lib/stores/roster";

const classId = "0192a000-0000-7000-8000-000000000021";

const listed: Roster = {
    class_id: classId,
    capacity: 4,
    entries: [
        {
            seat_no: 1,
            student_id: "0192a000-0000-7000-8000-000000000011",
            student_name: "Adi Tan",
            confirmed_at: "2026-08-11T08:00:00.000Z",
        },
    ],
};

/** A client that answers with one roster, or refuses. */
function stubClient(answer: Roster | Error) {
    const calls: string[] = [];

    return {
        calls,
        client: {
            async request<T>(request: { method: string; path: string }): Promise<T> {
                calls.push(request.path);

                if (answer instanceof Error) {
                    throw answer;
                }

                return answer as T;
            },
        },
    };
}

describe("the roster store", () => {
    test("integration: it reads one class's roster and holds it", async () => {
        const stub = stubClient(listed);
        const store = createRosterStore({ client: stub.client });

        await store.load(classId);

        expect(stub.calls).toEqual([rosterPath(classId)]);
        expect(get(store).roster?.entries).toHaveLength(1);
        expect(get(store).failure).toBe("");
    });

    test("behaviour: a refusal for the role is told apart from any other failure", async () => {
        // The two want different screens. A parent who typed the route should be
        // told plainly that it is not for them, and a teacher whose class does
        // not exist should be told that instead.
        const refused = stubClient(new ApiError("Forbidden", "forbidden_role", 403));
        const forbiddenStore = createRosterStore({ client: refused.client });

        await forbiddenStore.load(classId);

        expect(get(forbiddenStore).forbidden).toBe(true);
        expect(get(forbiddenStore).roster).toBeNull();

        const missing = stubClient(new ApiError("InvalidRequest", "invalid_request", 400));
        const missingStore = createRosterStore({ client: missing.client });

        await missingStore.load(classId);

        expect(get(missingStore).forbidden).toBe(false);
        expect(get(missingStore).failure).not.toBe("");
    });

    test("edge: a failed read leaves nothing of the previous roster on screen", async () => {
        // Unlike the class list, a stale roster is not better than none. It
        // carries other families' names, so a refusal has to clear it rather
        // than leave it up.
        const stub = stubClient(listed);
        const store = createRosterStore({ client: stub.client });

        await store.load(classId);

        expect(get(store).roster).not.toBeNull();

        const refused = createRosterStore({
            client: stubClient(new ApiError("Forbidden", "forbidden_role", 403)).client,
        });

        await refused.load(classId);

        expect(get(refused).roster).toBeNull();
    });

    test("edge: a failure that is not from the api still leaves a sentence", async () => {
        const store = createRosterStore({
            client: {
                async request<T>(): Promise<T> {
                    throw new TypeError("the network went away");
                },
            },
        });

        await store.load(classId);

        expect(get(store).failure).not.toBe("");
        expect(get(store).forbidden).toBe(false);
    });

    test("unit: resetting drops the names the screen was holding", async () => {
        const stub = stubClient(listed);
        const store = createRosterStore({ client: stub.client });

        await store.load(classId);
        store.reset();

        expect(get(store)).toEqual({ roster: null, loading: false, failure: "", forbidden: false });
    });

    test("unit: the path is built from the class and nothing else", () => {
        expect(rosterPath(classId)).toBe(`/api/v1/classes/${classId}/roster`);
    });
});
