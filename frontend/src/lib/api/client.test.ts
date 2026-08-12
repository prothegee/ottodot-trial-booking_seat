import { describe, expect, test } from "vitest";

import { ApiError } from "$lib/api/errors";
import { authPaths, createApiClient, ifNoneMatchHeader } from "$lib/api/client";
import { createFakeTransport, errorBody, type FakeHandler } from "$lib/api/transport_fake";
import type { Session } from "$lib/api/types";

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [{ id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 }],
};

/** Builds a client over a fake transport and records every hard sign out. */
function clientOver(handler: FakeHandler) {
    const transport = createFakeTransport(handler);
    const signOuts: ApiError[] = [];

    const api = createApiClient({
        transport,
        onSignOut: (failure) => {
            signOuts.push(failure);
        },
    });

    return { api, transport, signOuts };
}

describe("the api client", () => {
    test("integration: signing in asks who signed in, rather than assuming", async () => {
        // The api answers a login with two cookies and nothing else, so the
        // session has to be fetched separately.
        const { api, transport } = clientOver((request) => {
            if (request.path === authPaths.login) {
                return { status: 204 };
            }

            return { status: 200, body: seededSession };
        });

        await api.login("ada@example.test", "otto123");
        const session = await api.me();

        expect(session.display_name).toBe("Ada Tan");
        expect(transport.callsTo("POST", authPaths.login)).toHaveLength(1);
        expect(transport.callsTo("GET", authPaths.me)).toHaveLength(1);
    });

    test("integration: the sign in body carries both the email and the password", async () => {
        const { api, transport } = clientOver(() => ({ status: 204 }));

        await api.login("ada@example.test", "otto123");

        const [sent] = transport.callsTo("POST", authPaths.login);

        expect(sent.body).toEqual({ email: "ada@example.test", password: "otto123" });
    });

    test("unit: a 304 is a success carrying no body, never a failure", async () => {
        const { api } = clientOver(() => ({ status: 304 }));

        await expect(api.request({ method: "GET", path: "/api/v1/classes" })).resolves.toBeUndefined();
    });

    test("unit: a typed failure reaches the caller as an ApiError", async () => {
        const { api } = clientOver(() => ({ status: 409, body: errorBody("class_full") }));

        await expect(api.request({ method: "POST", path: "/api/v1/bookings" })).rejects.toMatchObject({
            kind: "ClassFull",
            code: "class_full",
        });
    });

    test("edge: an expired token is refreshed and the call retried exactly once", async () => {
        const { api, transport } = clientOver((request, callIndex) => {
            if (request.path === authPaths.refresh) {
                return { status: 204 };
            }

            // The first attempt is expired, the retry after the refresh works.
            return callIndex === 0
                ? { status: 401, body: errorBody("token_expired") }
                : { status: 200, body: seededSession };
        });

        const session = await api.me();

        expect(session.display_name).toBe("Ada Tan");
        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);
        expect(transport.callsTo("GET", authPaths.me)).toHaveLength(2);
    });

    test("edge: a call that is still unauthorised after the retry is not retried again", async () => {
        // Never a loop. The second refusal ends the session instead of
        // starting another refresh.
        const { api, transport, signOuts } = clientOver((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 204 };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        await expect(api.me()).rejects.toBeInstanceOf(ApiError);

        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);
        expect(transport.callsTo("GET", authPaths.me)).toHaveLength(2);
        expect(signOuts).toHaveLength(1);
    });

    test("edge: a refresh that is refused signs out once and does not retry the call", async () => {
        const { api, transport, signOuts } = clientOver((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 401, body: errorBody("token_reused") };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        await expect(api.me()).rejects.toMatchObject({ code: "token_reused" });

        expect(transport.callsTo("GET", authPaths.me)).toHaveLength(1);
        expect(signOuts).toHaveLength(1);
        expect(signOuts[0].code).toBe("token_reused");
    });

    test("edge: an invalid token signs out without attempting a refresh", async () => {
        // token_invalid means the signature failed or the token is denylisted.
        // Refreshing would be asking the same broken session for a new one.
        const { api, transport, signOuts } = clientOver(() => ({
            status: 401,
            body: errorBody("token_invalid"),
        }));

        await expect(api.me()).rejects.toMatchObject({ code: "token_invalid" });

        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(0);
        expect(signOuts).toHaveLength(1);
    });

    test("edge: an ordinary failure never signs the parent out", async () => {
        const { api, signOuts } = clientOver(() => ({ status: 409, body: errorBody("already_booked") }));

        await expect(api.request({ method: "POST", path: "/api/v1/bookings" })).rejects.toBeInstanceOf(ApiError);

        expect(signOuts).toHaveLength(0);
    });

    test("unit: a conditional get sends the stored tag verbatim", async () => {
        // The tag is the api's own string. This client never parses one,
        // compares two, or reasons about what is inside.
        const { api, transport } = clientOver(() => ({ status: 304 }));

        await api.conditionalGet("/api/v1/classes", "W/\"v41-gzip\"");

        expect(transport.calls[0].headers?.[ifNoneMatchHeader]).toBe("W/\"v41-gzip\"");
    });

    test("unit: a 304 comes back as unchanged, carrying no body", async () => {
        const { api } = clientOver(() => ({ status: 304 }));

        const answer = await api.conditionalGet("/api/v1/classes", "\"v41\"");

        expect(answer.notModified).toBe(true);
        expect(answer.body).toBeUndefined();
    });

    test("unit: a 200 comes back with the body and the new tag", async () => {
        const { api } = clientOver(() => ({
            status: 200,
            body: { classes: [] },
            headers: { etag: "\"v42\"" },
        }));

        const answer = await api.conditionalGet<{ classes: unknown[] }>("/api/v1/classes", "\"v41\"");

        expect(answer.notModified).toBe(false);
        expect(answer.body).toEqual({ classes: [] });
        expect(answer.etag).toBe("\"v42\"");
    });

    test("edge: an empty tag sends no header, because that is a different question", async () => {
        const { api, transport } = clientOver(() => ({ status: 200, body: { classes: [] } }));

        await api.conditionalGet("/api/v1/classes", "");

        expect(transport.calls[0].headers?.[ifNoneMatchHeader]).toBeUndefined();
    });

    test("edge: an answer with no tag reports an empty one rather than throwing", async () => {
        const { api } = clientOver(() => ({ status: 200, body: { classes: [] } }));

        const answer = await api.conditionalGet("/api/v1/classes", "");

        expect(answer.etag).toBe("");
    });

    test("edge: a conditional get takes the refresh and the one retry like any other call", async () => {
        const { api, transport } = clientOver((request, callIndex) => {
            if (request.path === authPaths.refresh) {
                return { status: 204 };
            }

            return callIndex === 0
                ? { status: 401, body: errorBody("token_expired") }
                : { status: 304 };
        });

        const answer = await api.conditionalGet("/api/v1/classes", "\"v41\"");

        expect(answer.notModified).toBe(true);
        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);
        expect(transport.callsTo("GET", "/api/v1/classes")).toHaveLength(2);
    });

    test("edge: a refused sign in is not treated as an expired session", async () => {
        // A wrong email or a wrong password cannot be fixed by refreshing
        // anything.
        const { api, transport } = clientOver(() => ({ status: 400, body: errorBody("invalid_request") }));

        await expect(api.login("nobody@example.test", "otto123")).rejects.toMatchObject({
            kind: "InvalidRequest",
        });

        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(0);
    });

    test("integration: asking who is signed in answers the session when there is one", async () => {
        const { api } = clientOver(() => ({ status: 200, body: seededSession }));

        expect(await api.whoIsSignedIn()).toMatchObject({ display_name: "Ada Tan" });
    });

    test("behaviour: nobody signed in is an answer, not a session ending", async () => {
        // This is asked on every page load, including the ones that need no
        // session. Reporting a first visit as a sign out would send somebody
        // reading `/status` to the sign-in screen.
        const { api, signOuts } = clientOver(() => ({
            status: 401,
            body: errorBody("token_invalid"),
        }));

        expect(await api.whoIsSignedIn()).toBeNull();
        expect(signOuts).toHaveLength(0);
    });

    test("behaviour: an access token that aged out is refreshed rather than reported", async () => {
        // The reload case. The parent is still signed in, and only the short
        // lived half of the session expired while the tab was closed.
        let refreshed = false;

        const { api, transport, signOuts } = clientOver((request) => {
            if (request.path === authPaths.refresh) {
                refreshed = true;

                return { status: 204 };
            }

            if (refreshed) {
                return { status: 200, body: seededSession };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        expect(await api.whoIsSignedIn()).toMatchObject({ display_name: "Ada Tan" });
        expect(transport.callsTo("GET", authPaths.me)).toHaveLength(2);
        expect(signOuts).toHaveLength(0);
    });

    test("edge: a refresh that fails is a session that really ended, and is reported", async () => {
        const { api, signOuts } = clientOver((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 401, body: errorBody("token_reused") };
            }

            return { status: 401, body: errorBody("token_expired") };
        });

        expect(await api.whoIsSignedIn()).toBeNull();
        expect(signOuts).toHaveLength(1);
        expect(signOuts[0].code).toBe("token_reused");
    });

    test("edge: an unreachable api answers nobody rather than throwing", async () => {
        // It runs on mount. A rejection there would be an unhandled one, and the
        // page would render with no header for a parent who is signed in.
        const { api } = clientOver(() => {
            throw new TypeError("network down");
        });

        expect(await api.whoIsSignedIn()).toBeNull();
    });
});
