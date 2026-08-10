import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { auth } from "$lib/stores/auth";
import type { Session } from "$lib/api/types";

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [{ id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 }],
};

describe("the auth store", () => {
    beforeEach(() => {
        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

    test("unit: signing in records what the screen needs", () => {
        auth.signIn(seededSession);

        const state = get(auth);

        expect(state.session?.display_name).toBe("Ada Tan");
        expect(state.session?.role).toBe("parent");
        expect(state.session?.children).toHaveLength(1);
        expect(state.signedOutReason).toBeNull();
    });

    test("unit: signing out drops the session and records why", () => {
        auth.signIn(seededSession);
        auth.signOut("token_reused");

        const state = get(auth);

        expect(state.session).toBeNull();
        expect(state.signedOutReason).toBe("token_reused");
    });

    test("unit: acknowledging the notice clears it without signing anyone in", () => {
        auth.signOut("session_ended");
        auth.acknowledgeNotice();

        const state = get(auth);

        expect(state.signedOutReason).toBeNull();
        expect(state.session).toBeNull();
    });

    test("edge: the store holds no email and no token, asserted on its serialized form", () => {
        // The check is on the serialized state rather than on named fields,
        // because the risk is a field nobody thought to look for.
        auth.signIn({
            ...seededSession,
            ...({ email: "ada@example.test", access_token: "header.payload.signature" } as object),
        } as Session);

        const serialized = JSON.stringify(get(auth));

        expect(serialized).not.toContain("ada@example.test");
        expect(serialized).not.toContain("access_token");
        expect(serialized).not.toContain("header.payload.signature");
        expect(serialized).toContain("Ada Tan");
    });

    test("edge: a child arriving with extra fields is copied down to what is displayed", () => {
        auth.signIn({
            ...seededSession,
            children: [
                {
                    ...seededSession.children[0],
                    ...({ medical_notes: "peanut allergy" } as object),
                },
            ],
        });

        expect(JSON.stringify(get(auth))).not.toContain("peanut allergy");
    });

    test("edge: a session with no children is held without failing", () => {
        auth.signIn({ ...seededSession, children: undefined as unknown as Session["children"] });

        expect(get(auth).session?.children).toEqual([]);
    });

    test("edge: nothing is written to session storage", () => {
        // The session itself lives in HttpOnly cookies this code cannot read.
        // A copy on disk would be the only durable trace of the parent in the
        // browser, for no benefit.
        sessionStorage.clear();
        auth.signIn(seededSession);

        expect(sessionStorage.length).toBe(0);
    });
});
