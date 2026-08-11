import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Readiness, VersionIdentity } from "$lib/api/types";
import { api } from "$lib/session/client";
import { readinessPath, status, versionPath } from "$lib/stores/status";
import StatusPage from "../routes/status/+page.svelte";

/**
 * Simulation F15: the status route reflects backend readiness.
 *
 *     open /status
 *     GET /version and GET /readyz
 *     all ok      green, every dependency listed ok
 *     replica down amber, the replica reported degraded
 *     503          red, unready
 *     poll every fifteen seconds while open, and stop on the way out
 *
 * Asserts: build identity renders from /version, each dependency renders its own
 * row, the amber case is distinct from the red case, and polling stops when the
 * route is left.
 *
 * The amber case is the one worth the most. A replica that has fallen behind
 * costs a class list some accuracy and costs nobody a seat, because no seat is
 * ever decided from it. Showing that the same way as an unready service would
 * send somebody looking for a problem that is not costing anything.
 */

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

const identity: VersionIdentity = {
    service: "ottodot-trial-booking-api",
    version: "0.1.0",
    commit: "6b30337",
    built_at: "2026-08-11T09:00:00Z",
    runtime: "go1.26.5",
};

/** Answers the two operations routes, whichever way a case wants each one. */
function answerWith(readiness: Readiness | ApiError) {
    vi.mocked(api.request).mockImplementation(async (request: { method: string; path: string }) => {
        if (request.path === versionPath) {
            return identity as never;
        }

        if (request.path === readinessPath) {
            if (readiness instanceof ApiError) {
                throw readiness;
            }

            return readiness as never;
        }

        throw new Error(`nothing should call ${request.path}`);
    });
}

describe("simulation F15: the status route reflects backend readiness", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.mocked(api.request).mockReset();

        status.close();
        status.reset();
    });

    afterEach(() => {
        status.close();
        vi.useRealTimers();
    });

    test("integration: everything healthy is green with every dependency listed", async () => {
        answerWith({
            status: "ready",
            checks: { postgres_primary: "ok", postgres_replica: "ok", redis: "ok" },
        });

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("readiness-dot").getAttribute("data-status")).toBe("ready");
        });

        for (const dependency of ["postgres_primary", "postgres_replica", "redis"]) {
            expect(screen.getByTestId(`status-dependency-${dependency}`)).toBeInTheDocument();
        }
    });

    test("integration: build identity renders from the version route", async () => {
        answerWith({ status: "ready", checks: { postgres_primary: "ok" } });

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("backend-version").textContent).toBe("0.1.0");
        });

        expect(screen.getByTestId("backend-commit").textContent).toBe("6b30337");
        expect(screen.getByTestId("backend-runtime").textContent).toBe("go1.26.5");
        expect(screen.getByTestId("backend-service").textContent).toBe(identity.service);
    });

    test("behaviour: a fallen replica is amber and says which dependency it was", async () => {
        answerWith({
            status: "degraded",
            checks: { postgres_primary: "ok", postgres_replica: "down", redis: "ok" },
        });

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("readiness-dot").getAttribute("data-status")).toBe("degraded");
        });

        expect(screen.getByTestId("status-dependency-postgres_replica").textContent).toContain("down");
    });

    test("behaviour: an unready service is red, and that is a different state from amber", async () => {
        // The api answers unready with a 503 carrying the report. Treating the
        // status as a failure would throw away the only information this screen
        // exists to show.
        answerWith(
            new ApiError("Unavailable", "dependency_unavailable", 503, 0, "", "", {
                status: "unavailable",
                checks: { postgres_primary: "down", postgres_replica: "ok", redis: "ok" },
            }),
        );

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("readiness-dot").getAttribute("data-status")).toBe("unavailable");
        });

        expect(screen.getByTestId("status-dependency-postgres_primary").textContent).toContain("down");
    });

    test("behaviour: it polls while open and stops when the screen is left", async () => {
        // The only timer in this client. One that outlives its screen is a
        // request every fifteen seconds for a page nobody is looking at.
        answerWith({ status: "ready", checks: { postgres_primary: "ok" } });

        const screenUnderTest = render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("readiness-dot")).toBeInTheDocument();
        });

        const afterFirst = vi.mocked(api.request).mock.calls.length;

        await vi.advanceTimersByTimeAsync(31_000);

        const whileOpen = vi.mocked(api.request).mock.calls.length;

        expect(whileOpen).toBeGreaterThan(afterFirst);

        screenUnderTest.unmount();

        await vi.advanceTimersByTimeAsync(60_000);

        expect(vi.mocked(api.request).mock.calls.length).toBe(whileOpen);
    });

    test("edge: a backend that answers nothing at all is a different message from an unready one", async () => {
        // One is a service telling the truth about being broken. The other is a
        // service that is not there, and a screen that showed the same thing for
        // both would be lying about one of them.
        vi.mocked(api.request).mockRejectedValue(new ApiError("Unavailable", "missing_envelope", 0));

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("status-failure")).toBeInTheDocument();
        });

        expect(screen.getByTestId("readiness-dot").getAttribute("data-status")).toBe("unknown");
    });

    test("edge: the readiness state is readable without seeing a colour", async () => {
        // Three states carried by a dot alone would say nothing to somebody who
        // cannot see it, and readiness is exactly the kind of thing a colour is
        // the worst way to carry.
        answerWith({ status: "degraded", checks: { postgres_replica: "down" } });

        render(StatusPage);

        await waitFor(() => {
            expect(screen.getByTestId("readiness-dot").textContent).toContain("Degraded");
        });
    });
});
