/**
 * The transport every test uses.
 *
 * It is a separate file from the real one so it never reaches a production
 * bundle, and so the two can never quietly grow into each other.
 *
 * The important property is that it records each call before answering it. That
 * is what lets a test assert not only what a parent saw, but that a request was
 * never sent at all, which is how the single-flight refresh and, later, the
 * fresh-cache case are proven.
 */
import type { Transport, TransportRequest, TransportResponse } from "$lib/api/transport";

/** What the handler gives back for one call. */
export interface FakeReply {
    status: number;
    body?: unknown;
    headers?: Record<string, string>;
}

/** Decides the answer for one call. The index counts from zero. */
export type FakeHandler = (request: TransportRequest, callIndex: number) => FakeReply | Promise<FakeReply>;

/** A transport that answers from a handler and remembers everything. */
export interface FakeTransport extends Transport {
    /** Every call, in the order it was made. */
    readonly calls: readonly TransportRequest[];

    /** Just the calls to one method and path. */
    callsTo(method: string, path: string): TransportRequest[];
}

/**
 * Builds the fake.
 *
 * Param:
 * handler - FakeHandler (what to answer, per call)
 *
 * Return:
 * - a transport that records every request it is given
 */
export function createFakeTransport(handler: FakeHandler): FakeTransport {
    const calls: TransportRequest[] = [];

    return {
        calls,

        callsTo(method: string, path: string): TransportRequest[] {
            return calls.filter((call) => call.method === method && call.path === path);
        },

        async send(request: TransportRequest): Promise<TransportResponse> {
            // Recorded before the handler runs, so a test watching for a second
            // refresh sees it even while the first one is still in flight.
            const callIndex = calls.length;
            calls.push(request);

            const reply = await handler(request, callIndex);

            return {
                status: reply.status,
                body: reply.body,
                headers: reply.headers ?? {},
            };
        },
    };
}

/** The failure envelope, in the shape the api sends it. */
export function errorBody(code: string, message = "the server said so", retryAfterSeconds?: number): unknown {
    return {
        error: {
            code,
            message,
            ...(retryAfterSeconds === undefined ? {} : { retry_after_seconds: retryAfterSeconds }),
        },
    };
}
