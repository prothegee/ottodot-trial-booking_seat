import { describe, expect, test } from "vitest";

import { authPaths, createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import type { ApiError } from "$lib/api/errors";

/**
 * Test 9: silent refresh, single flight.
 *
 *     client -> transport: three parallel calls
 *     transport -> client: 401 token_expired, three times
 *     client -> transport: POST /api/v1/auth/refresh, once only
 *     transport -> client: new cookies
 *     client -> transport: retry all three original calls
 *     transport -> client: 200, 200, 200
 *
 * Asserts: exactly one refresh call is recorded, all three originals are
 * retried once each, nothing is retried twice, and no sign out is reported, so
 * the parent sees no interruption.
 */

const classesPath = "/api/v1/classes";
const studentsPath = "/api/v1/students";
const bookingsPath = "/api/v1/bookings/0192a000-0000-7000-8000-000000000031";

describe("test 9: silent refresh", () => {
    test("behaviour: three expired calls cause one refresh and three retries", async () => {
        // Every business call is refused the first time and answered the
        // second. If the client refreshed per caller, the count below would be
        // three, and on a real backend the second rotation would be treated as
        // token reuse and end the session.
        const expired = new Set<string>();

        const transport = createFakeTransport((request) => {
            if (request.path === authPaths.refresh) {
                return { status: 204 };
            }

            if (!expired.has(request.path)) {
                expired.add(request.path);

                return { status: 401, body: errorBody("token_expired") };
            }

            return { status: 200, body: { path: request.path } };
        });

        const signOuts: ApiError[] = [];

        const api = createApiClient({
            transport,
            onSignOut: (failure) => {
                signOuts.push(failure);
            },
        });

        const answers = await Promise.all([
            api.request<{ path: string }>({ method: "GET", path: classesPath }),
            api.request<{ path: string }>({ method: "GET", path: studentsPath }),
            api.request<{ path: string }>({ method: "GET", path: bookingsPath }),
        ]);

        expect(answers.map((answer) => answer.path)).toEqual([classesPath, studentsPath, bookingsPath]);

        expect(transport.callsTo("POST", authPaths.refresh)).toHaveLength(1);

        expect(transport.callsTo("GET", classesPath)).toHaveLength(2);
        expect(transport.callsTo("GET", studentsPath)).toHaveLength(2);
        expect(transport.callsTo("GET", bookingsPath)).toHaveLength(2);

        expect(signOuts).toHaveLength(0);
    });

    test("behaviour: the refresh happens before any retry is sent", async () => {
        // Retrying before the new cookies arrive would fail again and burn the
        // one retry each caller gets.
        const order: string[] = [];
        const expired = new Set<string>();

        const transport = createFakeTransport((request) => {
            order.push(request.path);

            if (request.path === authPaths.refresh) {
                return { status: 204 };
            }

            if (!expired.has(request.path)) {
                expired.add(request.path);

                return { status: 401, body: errorBody("token_expired") };
            }

            return { status: 200, body: {} };
        });

        const api = createApiClient({ transport, onSignOut: () => {} });

        await Promise.all([
            api.request({ method: "GET", path: classesPath }),
            api.request({ method: "GET", path: studentsPath }),
        ]);

        const refreshAt = order.indexOf(authPaths.refresh);
        const retriesAfterTheRefresh = order.slice(refreshAt + 1);

        expect(refreshAt).toBeGreaterThan(-1);
        expect(retriesAfterTheRefresh).toEqual([classesPath, studentsPath]);
    });
});
