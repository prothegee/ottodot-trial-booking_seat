/**
 * What this client is allowed to report about itself.
 *
 * Every field on every event is a label value from a fixed list, or a duration.
 * There is no identifier here, no free text, and no place to put either, and
 * that absence is the design rather than an omission: the backend turns these
 * into Prometheus series, and a label taken from a booking would be one series
 * per booking on a system with no access control in front of it.
 *
 * The backend checks all of this again on the way in. That duplication is
 * deliberate: the telemetry endpoint is the one place the api takes label values
 * from outside itself, and a browser is somebody else's computer.
 */

/** The kinds of event the api accepts. */
export type TelemetryKind = "page_load" | "api_error" | "funnel_step" | "cache_lookup";

/**
 * The client routes a series is kept for.
 *
 * They are route patterns rather than paths, so `/booking/[bookingId]` is one
 * series and not one per booking. Anything not on this list is dropped by the
 * backend, so adding a screen means adding it here and there.
 */
export type TelemetryRoute =
    | "/"
    | "/sign-in"
    | "/book/[classId]"
    | "/pay/[bookingId]"
    | "/booking/[bookingId]"
    | "/bookings"
    | "/roster/[classId]"
    | "/status";

/** How far into a booking a parent got. */
export type FunnelStep = "list" | "hold" | "pay" | "confirmed";

/** Which tier of this client's own cache served a read. */
export type CacheResult = "fresh" | "stale" | "revalidated" | "miss";

/** One thing the client saw. */
export interface TelemetryEvent {
    kind: TelemetryKind;
    route?: TelemetryRoute;
    code?: string;
    step?: FunnelStep;
    result?: CacheResult;
    seconds?: number;
}

/** What one post carries. */
export interface TelemetryBatch {
    events: TelemetryEvent[];
}

/** Where a batch is posted. */
export const telemetryPath = "/api/v1/telemetry";

/**
 * The most events one post may carry.
 *
 * It matches the backend's own cap. A batch over it is refused whole rather than
 * truncated, so the emitter splits rather than sending something that will be
 * thrown away.
 */
export const maxBatchEvents = 50;

/** Records one client route becoming usable. */
export function pageLoadEvent(route: TelemetryRoute, seconds: number): TelemetryEvent {
    return { kind: "page_load", route, seconds };
}

/**
 * Records one typed api failure a parent was shown.
 *
 * The code is passed through untouched. It came from the api in the first place,
 * so inventing a client side vocabulary for it would only create two names for
 * the same thing and one place for them to disagree.
 */
export function apiErrorEvent(code: string): TelemetryEvent {
    return { kind: "api_error", code };
}

/** Records one step a parent reached. */
export function funnelEvent(step: FunnelStep): TelemetryEvent {
    return { kind: "funnel_step", step };
}

/** Records one read from this client's own cache. */
export function cacheEvent(result: CacheResult): TelemetryEvent {
    return { kind: "cache_lookup", result };
}
