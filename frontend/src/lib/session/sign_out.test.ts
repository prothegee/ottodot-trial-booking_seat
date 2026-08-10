import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { goto } from "$app/navigation";
import { classListPath } from "$lib/cache/key";
import { classCache } from "$lib/session/cache";
import { hardSignOut, reasonForCode, signInRoute } from "$lib/session/sign_out";
import { auth } from "$lib/stores/auth";
import { booking } from "$lib/stores/booking";
import { classes } from "$lib/stores/classes";
import type { Session } from "$lib/api/types";

// The navigation is mocked because there is no router in a unit test. What is
// under test is that a sign out asks to leave, not how SvelteKit gets there.
vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [],
};

describe("a hard sign out", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        sessionStorage.clear();
        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

    test("integration: it drops the session, empties storage, and leaves for sign in", async () => {
        auth.signIn(seededSession);
        sessionStorage.setItem("classes", JSON.stringify([{ id: "a class" }]));

        await hardSignOut("token_reused");

        expect(get(auth).session).toBeNull();
        expect(sessionStorage.length).toBe(0);
        expect(goto).toHaveBeenCalledWith(signInRoute);
    });

    test("integration: nothing of the previous parent is left in the stores", async () => {
        // The class list is not personal, but the booking store holds a booking
        // and the child it is for. On a shared machine the next parent to sign
        // in on this tab must not find either of them still there.
        auth.signIn(seededSession);

        await hardSignOut("requested");

        expect(get(classes).classes).toEqual([]);
        expect(get(booking).booking).toBeNull();
        expect(get(booking).attemptKey).toBe("");
    });

    test("unit: the reason survives so the sign-in screen can say why", () => {
        auth.signOut("token_reused");

        expect(get(auth).signedOutReason).toBe("token_reused");
    });

    test("unit: a reused token gets its own reason, everything else is a plain ending", () => {
        // A reused refresh token is either a stale tab or someone else holding
        // a copy. Either way the parent deserves to be told rather than
        // quietly returned to the form.
        expect(reasonForCode("token_reused")).toBe("token_reused");
        expect(reasonForCode("token_invalid")).toBe("session_ended");
        expect(reasonForCode("token_expired")).toBe("session_ended");
    });

    test("edge: signing out with nothing in storage is not an error", async () => {
        await expect(hardSignOut("session_ended")).resolves.toBeUndefined();
    });

    test("edge: the cached class list is dropped from memory, not only from storage", async () => {
        // Clearing storage alone would leave the previous parent's list in a
        // map that outlives the sign out, because nothing reloads the page.
        classCache.save(classListPath, { classes: [] }, "\"v41\"");

        await hardSignOut("session_ended");

        expect(classCache.read(classListPath)).toBeNull();
    });

    test("edge: storage written by an earlier session never survives", async () => {
        // The one that would leak between two parents on a shared machine.
        sessionStorage.setItem("cache:classes", "[]");
        sessionStorage.setItem("cache:roster", "[]");

        await hardSignOut("session_ended");

        expect(sessionStorage.getItem("cache:classes")).toBeNull();
        expect(sessionStorage.getItem("cache:roster")).toBeNull();
    });
});
