/**
 * The telemetry emitter the application actually uses, wired once.
 *
 * It is a separate file from the emitter for the same reason the api client is
 * separate from its factory: everything else in this stack takes its collaborator
 * as an argument so a test can supply a fake, and this is the one place the real
 * api client is put behind it.
 *
 * The post goes through the api client rather than through fetch, so it carries
 * the session cookie and the origin header exactly like every other write. The
 * endpoint is behind the parent role on the backend, and an anonymous post is
 * refused.
 */
import { api } from "$lib/session/client";
import { createEmitter, telemetryPath } from "$lib/telemetry/emitter";
import type { TelemetryBatch } from "$lib/telemetry/event";

/** The application wide emitter. */
export const telemetry = createEmitter({
    post: (batch: TelemetryBatch) =>
        api.request({ method: "POST", path: telemetryPath, body: batch }),
});
