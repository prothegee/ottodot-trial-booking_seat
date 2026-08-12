import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { goto } from "$app/navigation";
import { signInRoute } from "$lib/session/sign_out";
import { requestedSignOut, type SignOutCaller } from "$lib/session/sign_out_request";
import { auth } from "$lib/stores/auth";
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
    children: [
        {
            id: "0192a000-0000-7000-8000-000000000011",
            full_name: "Nico Tan",
            grade_level: 3,
        },
    ],
};

/** A client that records the call and answers however the case needs. */
function callerThat(answer: () => Promise<void>): SignOutCaller & { calls: number } {
    const caller = {
        calls: 0,

        async logout(): Promise<void> {
            caller.calls += 1;

            await answer();
        },
    };

    return caller;
}

describe("a sign out the parent asked for", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        sessionStorage.clear();
        auth.signIn(seededSession);
    });

    test("integration: the api is told, then this device is cleared", async () => {
        const caller = callerThat(() => Promise.resolve());

        await requestedSignOut(caller);

        expect(caller.calls).toBe(1);
        expect(get(auth).session).toBeNull();
        expect(vi.mocked(goto)).toHaveBeenCalledWith(signInRoute);
    });

    test("behaviour: nothing about the parent is left behind", async () => {
        // The children are the part that matters. A shared machine is the whole
        // reason a sign out exists, and another family's names surviving it
        // would be the worst thing this client could leak.
        sessionStorage.setItem("classes", JSON.stringify([{ id: "a class" }]));

        await requestedSignOut(callerThat(() => Promise.resolve()));

        expect(get(auth).session).toBeNull();
        expect(sessionStorage.length).toBe(0);
    });

    test("behaviour: the sign-in screen is given nothing to explain", async () => {
        // Every other reason puts a notice on that screen. This one is the
        // parent doing what they meant to, so a banner would be noise.
        await requestedSignOut(callerThat(() => Promise.resolve()));

        expect(get(auth).signedOutReason).toBe("requested");
    });

    test("edge: an api that refuses still signs the parent out of this screen", async () => {
        // Somebody pressed sign out and walked away. Whatever the network did,
        // the screen they left behind must not still be their session.
        const caller = callerThat(() => Promise.reject(new Error("the api said no")));

        await requestedSignOut(caller);

        expect(caller.calls).toBe(1);
        expect(get(auth).session).toBeNull();
        expect(vi.mocked(goto)).toHaveBeenCalledWith(signInRoute);
    });

    test("edge: an api that never answers does not hang the sign out", async () => {
        const caller = callerThat(() => Promise.reject(new TypeError("Failed to fetch")));

        await expect(requestedSignOut(caller)).resolves.toBeUndefined();
        expect(get(auth).session).toBeNull();
    });

    test("edge: the api is asked once, not once per press", async () => {
        // The header disables the control while this runs, and this is the case
        // that says why: two logouts race the same refresh token.
        const caller = callerThat(() => Promise.resolve());

        await requestedSignOut(caller);

        expect(caller.calls).toBe(1);
    });
});
