import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Roster, Session } from "$lib/api/types";
import { api } from "$lib/session/client";
import { auth } from "$lib/stores/auth";
import { roster } from "$lib/stores/roster";
import ClassListPage from "../routes/+page.svelte";
import RosterPage from "../routes/roster/[classId]/+page.svelte";

/**
 * Simulation F11: the roster view.
 *
 *     a teacher opens the roster for a class
 *     the api answers with the confirmed students and their seat numbers
 *     the screen lists confirmed only, with capacity and seats used
 *
 * Asserts: only `confirmed` bookings appear, `pending_payment` and
 * `refund_required` never leak into the roster, seat numbers render in order,
 * the roster is never read from cache, and a parent role never sees the link.
 *
 * The last two are the ones that matter. This is the only screen in the whole
 * client that puts a child's name next to a seat, so a cached copy of it would
 * be the one place in the browser where another family's name outlives the
 * screen that showed it. And the link being hidden from a parent is a courtesy
 * rather than the rule: what actually refuses them is the api, which is why the
 * last case here drives the refusal rather than the hidden link.
 */

const classId = "0192a000-0000-7000-8000-000000000021";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$app/state", () => ({
    page: { params: { classId: "0192a000-0000-7000-8000-000000000021" } },
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

vi.mock("$lib/telemetry/report", () => ({
    reportPageLoad: vi.fn(),
    reportApiError: vi.fn(),
    reportFunnel: vi.fn(),
    reportCacheLookup: vi.fn(),
}));

/**
 * The roster the api sends.
 *
 * It carries three confirmed seats and nothing else, because that is what the
 * api sends: the roster query filters on `confirmed`, so a hold and a booking
 * waiting on a refund never reach this client at all. The case below proves the
 * screen has no way to ask for them either.
 */
const confirmedRoster: Roster = {
    class_id: classId,
    capacity: 4,
    entries: [
        {
            seat_no: 1,
            student_id: "0192a000-0000-7000-8000-000000000011",
            student_name: "Adi Tan",
            confirmed_at: "2026-08-11T08:00:00.000Z",
        },
        {
            seat_no: 2,
            student_id: "0192a000-0000-7000-8000-000000000012",
            student_name: "Bella Tan",
            confirmed_at: "2026-08-11T08:05:00.000Z",
        },
        {
            seat_no: 3,
            student_id: "0192a000-0000-7000-8000-000000000013",
            student_name: "Citra Santoso",
            confirmed_at: "2026-08-11T08:10:00.000Z",
        },
    ],
};

const teacher: Session = {
    parent_id: "0192a000-0000-7000-8000-0000000000ad",
    display_name: "Operations",
    role: "admin",
    children: [],
};

const parent: Session = {
    parent_id: "0192a000-0000-7000-8000-0000000000aa",
    display_name: "Alice Tan",
    role: "parent",
    children: [],
};

describe("simulation F11: the roster view", () => {
    beforeEach(() => {
        sessionStorage.clear();

        roster.reset();
        auth.signOut("requested");

        vi.mocked(api.request).mockReset().mockResolvedValue(confirmedRoster);
    });

    test("integration: the confirmed students render with their seat numbers", async () => {
        auth.signIn(teacher);

        render(RosterPage);

        await waitFor(() => {
            expect(screen.getByTestId("roster-table")).toBeInTheDocument();
        });

        const seats = screen.getAllByTestId("roster-seat").map((cell) => cell.textContent);

        expect(seats).toEqual(["1", "2", "3"]);

        for (const name of ["Adi Tan", "Bella Tan", "Citra Santoso"]) {
            expect(screen.getByText(name)).toBeInTheDocument();
        }
    });

    test("integration: capacity and seats used are both on screen", async () => {
        auth.signIn(teacher);

        render(RosterPage);

        await waitFor(() => {
            expect(screen.getByTestId("roster-summary")).toBeInTheDocument();
        });

        expect(screen.getByTestId("roster-summary").textContent).toContain("3 of 4");
    });

    test("edge: nothing that is not confirmed can reach the screen", async () => {
        // The api filters on confirmed, and this proves the client has no second
        // way in: the only rows rendered are the entries the api sent, and there
        // is no status field on the wire shape to render anything else from.
        auth.signIn(teacher);

        render(RosterPage);

        await waitFor(() => {
            expect(screen.getByTestId("roster-table")).toBeInTheDocument();
        });

        expect(screen.getAllByTestId("roster-row")).toHaveLength(confirmedRoster.entries.length);

        const rendered = screen.getByTestId("roster-table").textContent ?? "";

        for (const leak of ["pending_payment", "refund_required", "payment_failed", "expired"]) {
            expect(rendered).not.toContain(leak);
        }
    });

    test("behaviour: the roster is read through the api client and never through the cache", async () => {
        // Every other read in this client that can be cached is advisory. This
        // one carries a child's name, so a stored copy would outlive the screen
        // that showed it.
        const { classReader } = await import("$lib/session/cached_api");

        auth.signIn(teacher);

        render(RosterPage);

        await waitFor(() => {
            expect(vi.mocked(api.request)).toHaveBeenCalled();
        });

        expect(vi.mocked(classReader.read)).not.toHaveBeenCalled();
        expect(sessionStorage.length).toBe(0);
    });

    test("behaviour: a parent role never sees the link", async () => {
        const { classReader } = await import("$lib/session/cached_api");

        vi.mocked(classReader.read).mockResolvedValue({
            body: {
                classes: [
                    {
                        id: classId,
                        subject: "science",
                        title: "Science trial",
                        starts_at: "2026-08-20T09:00:00.000Z",
                        duration_minutes: 60,
                        capacity: 4,
                        seats_remaining: 2,
                    },
                ],
            },
            result: "miss",
            revalidation: null,
        });

        auth.signIn(parent);

        const asParent = render(ClassListPage);

        await waitFor(() => {
            expect(screen.getByTestId("class-card")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("class-card-roster")).toBeNull();

        asParent.unmount();

        auth.signIn(teacher);

        render(ClassListPage);

        await waitFor(() => {
            expect(screen.getByTestId("class-card-roster")).toBeInTheDocument();
        });

        expect(screen.getByTestId("class-card-roster").getAttribute("href")).toBe(`/roster/${classId}`);
    });

    test("edge: a parent who types the route is refused by the api and told plainly", async () => {
        // The hidden link is a courtesy. This is the rule, and it is enforced on
        // the other side of the network where a developer tools window cannot
        // reach it.
        auth.signIn(parent);

        vi.mocked(api.request).mockRejectedValue(new ApiError("Forbidden", "forbidden_role", 403));

        render(RosterPage);

        await waitFor(() => {
            expect(screen.getByTestId("roster-forbidden")).toBeInTheDocument();
        });

        expect(screen.queryByTestId("roster-table")).toBeNull();
    });

    test("edge: a class with nobody confirmed says so rather than rendering an empty table", async () => {
        auth.signIn(teacher);

        vi.mocked(api.request).mockResolvedValue({ ...confirmedRoster, entries: [] });

        render(RosterPage);

        await waitFor(() => {
            expect(screen.getByTestId("roster-empty")).toBeInTheDocument();
        });

        expect(screen.getByTestId("roster-summary").textContent).toContain("0 of 4");
    });
});
