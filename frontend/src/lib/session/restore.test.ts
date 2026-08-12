import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import type { Session } from "$lib/api/types";
import { restoreSession } from "$lib/session/restore";
import { auth } from "$lib/stores/auth";

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [{ id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 }],
};

describe("restoring a session from the cookies", () => {
    beforeEach(() => {
        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

    test("integration: a tab with valid cookies gets its parent back", async () => {
        // The whole point. Nothing in this client can read an HttpOnly cookie,
        // so without this a signed-in parent opening a second tab is a stranger.
        const found = await restoreSession({
            whoIsSignedIn: vi.fn(() => Promise.resolve(seededSession)),
        });

        expect(found).toBe(true);
        expect(get(auth).session?.display_name).toBe("Ada Tan");
    });

    test("behaviour: nobody signed in is reported, not thrown", async () => {
        const found = await restoreSession({ whoIsSignedIn: vi.fn(() => Promise.resolve(null)) });

        expect(found).toBe(false);
        expect(get(auth).session).toBeNull();
    });

    test("behaviour: a first visit is not told that a session ended", async () => {
        // The question is asked on every page load, including the ones that need
        // no session at all. Answering "nobody" would otherwise greet a visitor
        // of `/status` with a warning about something that never happened.
        await restoreSession({ whoIsSignedIn: vi.fn(() => Promise.resolve(null)) });

        expect(get(auth).signedOutReason).toBeNull();
    });

    test("edge: a notice the api client already raised is left where it is", async () => {
        // A refresh token used twice is reported by the client itself. This
        // function only records who was found, and clearing that notice would
        // hide a session used somewhere else.
        auth.signOut("token_reused");

        await restoreSession({ whoIsSignedIn: vi.fn(() => Promise.resolve(null)) });

        expect(get(auth).signedOutReason).toBe("token_reused");
    });

    test("edge: a restored session clears the notice that sent the parent away", async () => {
        auth.signOut("session_ended");

        await restoreSession({ whoIsSignedIn: vi.fn(() => Promise.resolve(seededSession)) });

        expect(get(auth).signedOutReason).toBeNull();
        expect(get(auth).session?.display_name).toBe("Ada Tan");
    });
});
