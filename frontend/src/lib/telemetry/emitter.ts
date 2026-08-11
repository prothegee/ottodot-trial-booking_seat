/**
 * The one thing that posts telemetry, and the one rule it never breaks.
 *
 * Monitoring must never break a booking. Every failure here is swallowed, every
 * post is fire and forget, and nothing a screen does waits on this file. A
 * parent whose booking failed because the metrics endpoint was slow would be the
 * worst possible outcome of measuring anything at all.
 *
 * Events are batched rather than posted one at a time, because a booking flow
 * produces a handful of them in a few seconds and eight requests to say so would
 * cost more than the thing being measured.
 */
import { maxBatchEvents, telemetryPath, type TelemetryBatch, type TelemetryEvent } from "$lib/telemetry/event";

/** How long events are held before they go out. */
export const flushIntervalMs = 10_000;

/**
 * The most events held before a post is forced.
 *
 * It is the backend's batch cap. Holding more would mean either splitting on the
 * way out or having a post refused whole, and a queue that grows without bound
 * while the api is unreachable is a memory leak in a tab somebody left open.
 */
export const maxQueued = maxBatchEvents;

/** How a batch actually leaves the browser. */
export type PostBatch = (batch: TelemetryBatch) => Promise<unknown>;

/** What the emitter is built with. */
export interface EmitterOptions {
    /** Where a batch goes. */
    post: PostBatch;

    /** Overrides the flush period, so a test does not wait ten seconds. */
    intervalMs?: number;
}

/** What an emitter can do. */
export interface Emitter {
    /** Queues one event. It never throws and never returns a promise to await. */
    record(event: TelemetryEvent): void;

    /**
     * Sends whatever is queued, now.
     *
     * Return:
     * - a promise that resolves when the post has settled, whichever way it
     *   went. It exists for tests and for a deliberate flush before a sign out,
     *   and no screen awaits it
     */
    flush(): Promise<void>;

    /** Stops the timer and throws away anything still queued. */
    stop(): void;

    /** How many events are waiting. For tests, and for nothing else. */
    queued(): number;
}

/**
 * Builds an emitter.
 *
 * Note:
 * - the timer only runs while something is queued. An idle tab with this client
 *   open should cost nothing at all, and a ten second interval that fires
 *   forever on an empty queue is a wake up every ten seconds for no reason.
 * - a failed post throws its batch away rather than retrying it. Retrying would
 *   mean an unreachable api turns into a growing queue and then into repeated
 *   requests at exactly the moment the api is least able to answer them. Losing
 *   a page load timing is not worth that.
 *
 * Param:
 * options - EmitterOptions (how a batch is posted, and how often)
 *
 * Return:
 * - the emitter
 */
export function createEmitter(options: EmitterOptions): Emitter {
    const intervalMs = options.intervalMs ?? flushIntervalMs;

    let queue: TelemetryEvent[] = [];
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    function clearTimer(): void {
        if (timer !== undefined) {
            clearTimeout(timer);
            timer = undefined;
        }
    }

    function scheduleFlush(): void {
        if (timer !== undefined || stopped) {
            return;
        }

        timer = setTimeout(() => {
            timer = undefined;

            void flush();
        }, intervalMs);
    }

    async function flush(): Promise<void> {
        clearTimer();

        if (queue.length === 0) {
            return;
        }

        const batch: TelemetryBatch = { events: queue };

        // The queue is emptied before the post rather than after it. A post that
        // takes a moment must not let the same events go out twice, and a post
        // that fails must not leave them to be retried.
        queue = [];

        try {
            await options.post(batch);
        } catch {
            // Swallowed on purpose, and not even logged. A monitoring failure a
            // parent can see is a monitoring failure that has cost more than it
            // was ever going to save.
        }
    }

    return {
        record(event: TelemetryEvent): void {
            if (stopped) {
                return;
            }

            queue.push(event);

            if (queue.length >= maxQueued) {
                void flush();

                return;
            }

            scheduleFlush();
        },

        flush,

        stop(): void {
            stopped = true;
            queue = [];

            clearTimer();
        },

        queued(): number {
            return queue.length;
        },
    };
}

/** The path a batch is posted to, re-exported so a caller needs one import. */
export { telemetryPath };
