import { describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import type { ClassList, TrialClass } from "$lib/api/types";
import type { CacheLookupResult } from "$lib/cache/read_through";
import { classListPath, createClassesStore } from "$lib/stores/classes";

const scienceClass: TrialClass = {
    id: "0192a000-0000-7000-8000-000000000021",
    subject: "science",
    title: "Science Discovery",
    starts_at: "2026-08-14T09:00:00Z",
    duration_minutes: 60,
    capacity: 4,
    seats_remaining: 3,
};

/** One answer from a reader, in the shape the real one returns. */
interface StubbedRead {
    body: unknown;
    result: CacheLookupResult;
    revalidation?: Promise<void> | null;
}

/** A reader that answers from a script and records what it was asked for. */
function stubReader(answers: (StubbedRead | Error)[]) {
    const paths: string[] = [];
    let call = 0;

    return {
        paths,

        read<T>(path: string): Promise<{ body: T; result: CacheLookupResult; revalidation: Promise<void> | null }> {
            paths.push(path);

            const answer = answers[Math.min(call, answers.length - 1)];
            call += 1;

            if (answer instanceof Error) {
                return Promise.reject(answer);
            }

            return Promise.resolve({
                body: answer.body as T,
                result: answer.result,
                revalidation: answer.revalidation ?? null,
            });
        },
    };
}

const oneClass: ClassList = { classes: [scienceClass] };

describe("the class list store", () => {
    test("integration: a read fills the store from the cache reader", async () => {
        const reader = stubReader([{ body: oneClass, result: "miss" }]);
        const classes = createClassesStore({ reader });

        await classes.load();

        const state = get(classes);

        expect(state.classes).toHaveLength(1);
        expect(state.classes[0].title).toBe("Science Discovery");
        expect(state.loading).toBe(false);
        expect(state.failure).toBe("");
    });

    test("unit: the list is read from the one path the cache is allowed to hold", () => {
        const reader = stubReader([{ body: oneClass, result: "miss" }]);
        const classes = createClassesStore({ reader });

        void classes.load();

        expect(reader.paths).toEqual([classListPath]);
    });

    test("unit: which tier served the read is kept, so it can be counted later", async () => {
        const reader = stubReader([{ body: oneClass, result: "fresh" }]);
        const classes = createClassesStore({ reader });

        await classes.load();

        expect(get(classes).lastResult).toBe("fresh");
    });

    test("integration: a stale read hands back the revalidation so it can be awaited", async () => {
        // A screen ignores it. A test awaits it, which is how a background
        // confirmation is observed without a timer.
        let confirmed = false;
        const revalidation = Promise.resolve().then(() => {
            confirmed = true;
        });

        const reader = stubReader([{ body: oneClass, result: "stale", revalidation }]);
        const classes = createClassesStore({ reader });

        const handedBack = await classes.load();

        expect(handedBack).not.toBeNull();

        await handedBack;

        expect(confirmed).toBe(true);
    });

    test("edge: a failed read leaves the list that is already on screen", async () => {
        // It is no more wrong than it was a second ago, and blanking the page
        // would take away the one thing the parent could still act on.
        const reader = stubReader([
            { body: oneClass, result: "miss" },
            new ApiError("Unavailable", "internal_error", 500),
        ]);
        const classes = createClassesStore({ reader });

        await classes.load();
        await classes.load();

        const state = get(classes);

        expect(state.classes).toHaveLength(1);
        expect(state.failure).toBe("something went wrong on our side, your booking was not changed");
        expect(state.loading).toBe(false);
    });

    test("edge: a failure that is not from the api still leaves a sentence", async () => {
        const reader = stubReader([new Error("the network is on fire")]);
        const classes = createClassesStore({ reader });

        await classes.load();

        expect(get(classes).failure).toBe("the class list could not be loaded");
    });

    test("edge: a successful read clears the failure the previous one left", async () => {
        const reader = stubReader([
            new ApiError("Unavailable", "internal_error", 500),
            { body: oneClass, result: "miss" },
        ]);
        const classes = createClassesStore({ reader });

        await classes.load();
        await classes.load();

        expect(get(classes).failure).toBe("");
    });

    test("edge: an answer with no classes field reads as an empty list rather than throwing", async () => {
        // A backend that answers 200 with something unexpected must not blank
        // the screen with an exception.
        const reader = stubReader([{ body: {}, result: "miss" }]);
        const classes = createClassesStore({ reader });

        await classes.load();

        expect(get(classes).classes).toEqual([]);
        expect(get(classes).failure).toBe("");
    });

    test("edge: resetting empties everything, which is what a sign out needs", async () => {
        const reader = stubReader([{ body: oneClass, result: "miss" }]);
        const classes = createClassesStore({ reader });

        await classes.load();
        classes.reset();

        expect(get(classes)).toEqual({ classes: [], loading: false, failure: "", lastResult: null });
    });
});
