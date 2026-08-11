/**
 * Where the rest of the client reports what it saw.
 *
 * Four functions, one per event kind, over the wired emitter. Nothing outside
 * this file touches the emitter, which is what keeps the "monitoring never
 * breaks a booking" rule enforceable: there is one place a store or a screen can
 * report from, and none of these returns anything to wait on.
 *
 * A test replaces this module rather than the emitter, the same way it replaces
 * the cached api. That keeps a case about a booking free of a batching timer it
 * has no opinion about.
 */
import { telemetry } from "$lib/session/telemetry";
import {
    apiErrorEvent,
    cacheEvent,
    funnelEvent,
    pageLoadEvent,
    type CacheResult,
    type FunnelStep,
    type TelemetryRoute,
} from "$lib/telemetry/event";

/** Records one client route becoming usable. */
export function reportPageLoad(route: TelemetryRoute, seconds: number): void {
    telemetry.record(pageLoadEvent(route, seconds));
}

/**
 * Records one typed api failure a parent was shown.
 *
 * It is called where the failure is rendered rather than where it is caught,
 * which is the difference between "the api refused something" and "somebody was
 * told no". The second is the number worth a panel.
 */
export function reportApiError(code: string): void {
    telemetry.record(apiErrorEvent(code));
}

/** Records one step a parent reached. */
export function reportFunnel(step: FunnelStep): void {
    telemetry.record(funnelEvent(step));
}

/** Records one read from this client's own cache. */
export function reportCacheLookup(result: CacheResult): void {
    telemetry.record(cacheEvent(result));
}
