/**
 * How long a screen took to become usable, measured in the browser.
 *
 * Nothing on the server can see this number. A request duration ends when the
 * body is written, and what a parent experiences is that plus the network, the
 * parse, and the render. This is the only way that gap ever gets measured.
 *
 * It is a separate file from the reporting functions because it is a different
 * thing: those are where an event goes, and this is how one particular event's
 * number is arrived at.
 */
import { reportPageLoad } from "$lib/telemetry/report";
import type { TelemetryRoute } from "$lib/telemetry/event";

/** What produces the reading. Injected only by a test. */
export interface PageLoadOptions {
    /** A monotonic clock in milliseconds. Defaults to performance.now. */
    now?: () => number;

    /** Where the measurement goes. Defaults to the wired reporter. */
    report?: (route: TelemetryRoute, seconds: number) => void;
}

/**
 * Starts the timer for one screen.
 *
 * The returned function is called when the screen has something a parent can
 * act on, which is not the same as when it mounted. A class list that mounted
 * instantly and then waited two seconds for its data was not usable for two
 * seconds, and reporting the mount would say the opposite.
 *
 * Note:
 * - calling the returned function twice reports once. A screen whose data
 *   arrives in two parts would otherwise report the second part as a second page
 *   load, and the histogram would fill with numbers nobody meant.
 *
 * Param:
 * route - TelemetryRoute (the route pattern, never a path with an id in it)
 * options - PageLoadOptions (the clock and the reporter, injected by a test)
 *
 * Return:
 * - a function to call once, when the screen is usable
 */
export function measurePageLoad(route: TelemetryRoute, options: PageLoadOptions = {}): () => void {
    const now = options.now ?? (() => performance.now());
    const report = options.report ?? reportPageLoad;

    const startedAt = now();

    let reported = false;

    return () => {
        if (reported) {
            return;
        }

        reported = true;

        const elapsed = now() - startedAt;

        // A negative reading cannot happen with a monotonic clock and would be
        // meaningless if it did, so it is clamped rather than sent.
        report(route, Math.max(elapsed, 0) / 1000);
    };
}
