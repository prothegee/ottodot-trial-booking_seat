import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { createEmitter, maxQueued } from "$lib/telemetry/emitter";
import { funnelEvent, pageLoadEvent, type TelemetryBatch } from "$lib/telemetry/event";

describe("the telemetry emitter", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("integration: events are held and go out together", async () => {
        // A booking flow produces a handful of events in a few seconds. Eight
        // requests to say so would cost more than the thing being measured.
        const sent: TelemetryBatch[] = [];

        const emitter = createEmitter({
            post: async (batch) => {
                sent.push(batch);
            },
            intervalMs: 1000,
        });

        emitter.record(funnelEvent("list"));
        emitter.record(funnelEvent("hold"));
        emitter.record(funnelEvent("pay"));

        expect(sent).toHaveLength(0);

        await vi.advanceTimersByTimeAsync(1000);

        expect(sent).toHaveLength(1);
        expect(sent[0].events).toHaveLength(3);
    });

    test("edge: a failed post never surfaces to the parent", async () => {
        // The rule the whole file exists for. Monitoring must never break a
        // booking, so a post that fails is swallowed whole and nothing a screen
        // does can observe it happening.
        const emitter = createEmitter({
            post: async () => {
                throw new Error("the api is unreachable");
            },
            intervalMs: 1000,
        });

        emitter.record(funnelEvent("list"));

        // If the rejection escaped, either of these would reject and the case
        // would fail on an unhandled error rather than on an assertion.
        await vi.advanceTimersByTimeAsync(1000);

        await expect(emitter.flush()).resolves.toBeUndefined();
    });

    test("edge: a failed batch is thrown away rather than retried", async () => {
        // Retrying would turn an unreachable api into a growing queue and then
        // into repeated requests at exactly the moment the api is least able to
        // answer them. Losing a page load timing is not worth that.
        let attempts = 0;

        const emitter = createEmitter({
            post: async () => {
                attempts += 1;

                throw new Error("the api is unreachable");
            },
            intervalMs: 1000,
        });

        emitter.record(funnelEvent("list"));

        await vi.advanceTimersByTimeAsync(1000);
        await vi.advanceTimersByTimeAsync(5000);

        expect(attempts).toBe(1);
        expect(emitter.queued()).toBe(0);
    });

    test("edge: a full queue is sent at once rather than growing", async () => {
        // A queue that grows without bound while the api is unreachable is a
        // memory leak in a tab somebody left open.
        const sent: TelemetryBatch[] = [];

        const emitter = createEmitter({
            post: async (batch) => {
                sent.push(batch);
            },
            intervalMs: 60_000,
        });

        for (let index = 0; index < maxQueued; index += 1) {
            emitter.record(funnelEvent("list"));
        }

        await vi.advanceTimersByTimeAsync(0);

        expect(sent).toHaveLength(1);
        expect(sent[0].events).toHaveLength(maxQueued);
        expect(emitter.queued()).toBe(0);
    });

    test("edge: the same event never goes out twice", async () => {
        // The queue is emptied before the post rather than after it, so a post
        // that takes a moment cannot let a second flush send the same events.
        const sent: TelemetryBatch[] = [];

        const emitter = createEmitter({
            post: async (batch) => {
                sent.push(batch);

                await new Promise((resolve) => setTimeout(resolve, 100));
            },
            intervalMs: 1000,
        });

        emitter.record(pageLoadEvent("/", 0.4));

        const first = emitter.flush();
        const second = emitter.flush();

        await vi.advanceTimersByTimeAsync(200);
        await Promise.all([first, second]);

        expect(sent).toHaveLength(1);
    });

    test("unit: an idle emitter schedules nothing", async () => {
        // A ten second timer that fires forever on an empty queue is a wake up
        // every ten seconds for no reason at all.
        let posts = 0;

        const emitter = createEmitter({
            post: async () => {
                posts += 1;
            },
            intervalMs: 1000,
        });

        await vi.advanceTimersByTimeAsync(10_000);

        expect(posts).toBe(0);
        expect(emitter.queued()).toBe(0);
    });

    test("unit: a stopped emitter records nothing and sends nothing", async () => {
        let posts = 0;

        const emitter = createEmitter({
            post: async () => {
                posts += 1;
            },
            intervalMs: 1000,
        });

        emitter.record(funnelEvent("list"));
        emitter.stop();
        emitter.record(funnelEvent("hold"));

        await vi.advanceTimersByTimeAsync(5000);

        expect(posts).toBe(0);
        expect(emitter.queued()).toBe(0);
    });

    test("unit: flushing an empty queue posts nothing", async () => {
        let posts = 0;

        const emitter = createEmitter({
            post: async () => {
                posts += 1;
            },
            intervalMs: 1000,
        });

        await emitter.flush();

        expect(posts).toBe(0);
    });
});
