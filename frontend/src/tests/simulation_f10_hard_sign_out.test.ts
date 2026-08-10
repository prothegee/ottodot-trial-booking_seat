import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { goto } from "$app/navigation";
import { authPaths, createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import { hardSignOut, reasonForCode, signInRoute } from "$lib/session/sign_out";
import { auth } from "$lib/stores/auth";
import type { Session } from "$lib/api/types";

/**
 * Simulation F10: hard sign out on a reused token.
 *
 *     client -> transport: a call
 *     transport -> client: 401 token_expired
 *     client -> transport: refresh
 *     transport -> client: 401 token_reused
 *     client: clear the auth store, clear session storage
 *     client: route to sign in with a security notice
 *
 * Asserts: no retry loop, the auth store and the whole of sessionStorage are
 * cleared, and the reason survives so the sign-in screen can state it.
 */

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

const classesPath = "/api/v1/classes";

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [{ id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 }],
};

describe("simulation F10: hard sign out on a reused token", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        sessionStorage.clear();
        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

    test("behaviour: a reused refresh token ends the session everywhere at once", async () => {
        auth.signIn(seededSession);
        sessionStorage.setItem("cache:classes", JSON.stringify([{ id: "a class" }]));

        const transport = createFakeTransport((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 401, body: errorBody("token_reused") };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        const signOuts: string[] = [];

        const api = createApiClient({
            transport,
            onSignOut: (failure) => {
                signOuts.push(failure.code);

                return void hardSignOut(reasonForCode(failure.code));
            },
        });

        await expect(api.request({ method: "GET", path: classesPath })).rejects.toMatchObject({
            code: "token_reused",
        });

        // Waits for the sign out that the client deliberately does not block on.
        await vi.waitFor(() => {
            expect(goto).toHaveBeenCalledWith(signInRoute);
        });

        expect(signOuts).toEqual(["token_reused"]);
        expect(get(auth).session).toBeNull();
        expect(get(auth).signedOutReason).toBe("token_reused");
        expect(sessionStorage.length).toBe(0);
    });

    test("behaviour: there is no retry loop, the call is attempted once", async () => {
        const transport = createFakeTransport((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 401, body: errorBody("token_reused") };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        const api = createApiClient({
            transport,
            onSignOut: (failure) => void hardSignOut(reasonForCode(failure.code)),
        });

        await expect(api.request({ method: "GET", path: classesPath })).rejects.toBeTruthy();

        expect(transport.callsTo("GET", classesPath)).toHaveLength(1);
        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);
    });

    test("behaviour: three calls failing together sign the parent out once, not three times", async () => {
        // Three waiting callers must not each end the session. Beyond the
        // noise, three sign outs would mean three navigations away from
        // whatever the parent was looking at.
        const transport = createFakeTransport((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 401, body: errorBody("token_reused") };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        let signOuts = 0;

        const api = createApiClient({
            transport,
            onSignOut: (failure) => {
                signOuts += 1;

                return void hardSignOut(reasonForCode(failure.code));
            },
        });

        const outcomes = await Promise.allSettled([
            api.request({ method: "GET", path: "/api/v1/classes" }),
            api.request({ method: "GET", path: "/api/v1/students" }),
            api.request({ method: "GET", path: "/api/v1/bookings" }),
        ]);

        expect(outcomes.every((outcome) => outcome.status === "rejected")).toBe(true);
        expect(signOuts).toBe(1);
        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);
    });
});
