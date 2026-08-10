/**
 * The one place this client touches the network.
 *
 * Everything above it works against the Transport interface, which is what
 * makes every test in this stack run with no server, no browser, and no
 * containers. The fake lives in transport_fake.ts so it never reaches a
 * production bundle.
 */

/** One outgoing call, described without any knowledge of fetch. */
export interface TransportRequest {
    method: string;
    path: string;
    body?: unknown;
    headers?: Readonly<Record<string, string>>;
}

/** One answer, with the header names already lowercased. */
export interface TransportResponse {
    status: number;
    body: unknown;
    headers: Readonly<Record<string, string>>;
}

/** What the api client is handed at construction. */
export interface Transport {
    send(request: TransportRequest): Promise<TransportResponse>;
}

/** The shape of fetch, narrowed to what this file uses. */
type FetchLike = (input: string, init: RequestInit) => Promise<Response>;

/** Statuses that carry no body, so nothing tries to parse one. */
const statusesWithoutABody = new Set([204, 304]);

/**
 * Builds the transport that production uses.
 *
 * Note:
 * - every request sends `credentials: "include"`. That is the whole of this
 *   client's token handling: the cookies travel, and no code path reads,
 *   decodes, or stores either one.
 * - the request goes to the api's real origin rather than through a proxy on
 *   this host, because the api checks the Origin header on mutations and a
 *   proxy would send the wrong one.
 *
 * Param:
 * baseUrl - string (where the api lives, injected at build time)
 * send - FetchLike (handed in only so a test can watch what was sent)
 *
 * Return:
 * - a transport ready to be given to the api client
 */
export function createFetchTransport(
    baseUrl: string,
    send: FetchLike = (input, init) => fetch(input, init),
): Transport {
    return {
        async send(request: TransportRequest): Promise<TransportResponse> {
            const headers: Record<string, string> = {
                accept: "application/json",
                ...(request.headers ?? {}),
            };

            const init: RequestInit = {
                method: request.method,
                headers,
                credentials: "include",
            };

            if (request.body !== undefined) {
                headers["content-type"] = "application/json";
                init.body = JSON.stringify(request.body);
            }

            const response = await send(baseUrl + request.path, init);

            return {
                status: response.status,
                body: await readBody(response),
                headers: collectHeaders(response),
            };
        },
    };
}

/**
 * Reads a response body, and treats a body that is absent or unreadable as no
 * body rather than as a failure.
 *
 * A 204 has nothing to read by definition, and a 500 from a proxy arrives as
 * html. Neither should turn into an exception the caller has to guess about.
 */
async function readBody(response: Response): Promise<unknown> {
    if (statusesWithoutABody.has(response.status)) {
        return undefined;
    }

    const contentType = response.headers.get("content-type") ?? "";

    if (!contentType.includes("application/json")) {
        return undefined;
    }

    try {
        return await response.json();
    } catch {
        return undefined;
    }
}

/** Copies the response headers into a plain record with lowercase names. */
function collectHeaders(response: Response): Record<string, string> {
    const collected: Record<string, string> = {};

    response.headers.forEach((value, name) => {
        collected[name.toLowerCase()] = value;
    });

    return collected;
}
