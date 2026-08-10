import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import type { ClassList } from "$lib/api/types";
import { classReader } from "$lib/session/cached_api";
import { classes } from "$lib/stores/classes";
import ClassListPage from "./+page.svelte";

// The reader is replaced so this test is about the screen and the store, not
// about the network. The cache itself is covered by its own tests and by the
// simulations.
vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

const twoClasses: ClassList = {
    classes: [
        {
            id: "0192a000-0000-7000-8000-000000000021",
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 3,
        },
        {
            id: "0192a000-0000-7000-8000-000000000022",
            subject: "math",
            title: "Math Challenge",
            starts_at: "2026-08-15T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 0,
        },
    ],
};

/** Answers the next read with a body, on the tier named. */
function readAnswers(body: unknown, result = "miss"): void {
    vi.mocked(classReader.read).mockResolvedValue({ body, result, revalidation: null } as never);
}

describe("the class list screen", () => {
    beforeEach(() => {
        vi.mocked(classReader.read).mockReset();
        classes.reset();
    });

    test("integration: opening the screen reads the list and renders a card each", async () => {
        readAnswers(twoClasses);

        render(ClassListPage);

        await waitFor(() => {
            expect(screen.getAllByTestId("class-card")).toHaveLength(2);
        });

        expect(screen.getByRole("heading", { name: "Science Discovery" })).toBeInTheDocument();
        expect(screen.getByRole("heading", { name: "Math Challenge" })).toBeInTheDocument();
    });

    test("integration: a full class shows as full and offers no way in", async () => {
        readAnswers(twoClasses);

        render(ClassListPage);

        await waitFor(() => {
            expect(screen.getAllByTestId("class-card")).toHaveLength(2);
        });

        // One of the two has seats and the other does not, so exactly one book
        // link is the assertion rather than the absence of all of them.
        expect(screen.getAllByTestId("class-card-book")).toHaveLength(1);
        expect(screen.getByTestId("class-card-closed")).toBeInTheDocument();
    });

    test("edge: a read that fails says so rather than showing an empty catalogue", async () => {
        // An empty list and a failed read look the same on screen unless one of
        // them says which it is, and a parent who thinks there are no classes
        // does not come back.
        vi.mocked(classReader.read).mockRejectedValue(new Error("the network is on fire"));

        render(ClassListPage);

        await waitFor(() => {
            expect(screen.getByTestId("class-list-failure")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("class-list-empty")).not.toBeInTheDocument();
    });

    test("edge: a catalogue with nothing in it says so", async () => {
        readAnswers({ classes: [] });

        render(ClassListPage);

        await waitFor(() => {
            expect(screen.getByTestId("class-list-empty")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("class-card")).not.toBeInTheDocument();
    });

    test("edge: the screen reads through the cache and never through the api client", async () => {
        // A screen reaching past the reader is the one way the internal cache
        // can go wrong, so it is asserted rather than left to review.
        readAnswers(twoClasses);

        render(ClassListPage);

        await waitFor(() => {
            expect(classReader.read).toHaveBeenCalledWith("/api/v1/classes");
        });
    });
});
