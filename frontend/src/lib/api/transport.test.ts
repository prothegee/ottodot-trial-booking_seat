import { describe, expect, test } from "vitest";

import { createFetchTransport } from "$lib/api/transport";

/** Records what fetch was given and answers with whatever the test set up. */
function recordingFetch(reply: Response) {
    const seen: Array<{ url: string; init: RequestInit }> = [];

    const send = async (url: string, init: RequestInit): Promise<Response> => {
        seen.push({ url, init });

        return reply;
    };

    return { seen, send };
}

function jsonReply(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { "content-type": "application/json", etag: `"tag-${status}"` },
    });
}

describe("the fetch transport", () => {
    test("unit: every request sends the cookies", async () => {
        // This one line is the whole of this client's token handling. Without
        // it the api never sees a session and every call is a 401.
        const fetcher = recordingFetch(jsonReply(200, { ok: true }));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        await transport.send({ method: "GET", path: "/api/v1/auth/me" });

        expect(fetcher.seen[0].init.credentials).toBe("include");
    });

    test("unit: the path is appended to the injected base url", async () => {
        const fetcher = recordingFetch(jsonReply(200, {}));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        await transport.send({ method: "GET", path: "/api/v1/classes" });

        expect(fetcher.seen[0].url).toBe("http://api.test/api/v1/classes");
    });

    test("unit: a body is sent as json and the header says so", async () => {
        const fetcher = recordingFetch(jsonReply(200, {}));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        await transport.send({ method: "POST", path: "/api/v1/auth/login", body: { email: "a@example.test" } });

        const headers = fetcher.seen[0].init.headers as Record<string, string>;

        expect(headers["content-type"]).toBe("application/json");
        expect(fetcher.seen[0].init.body).toBe(JSON.stringify({ email: "a@example.test" }));
    });

    test("edge: a request with no body sends no content type", async () => {
        const fetcher = recordingFetch(jsonReply(200, {}));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        await transport.send({ method: "GET", path: "/api/v1/auth/me" });

        const headers = fetcher.seen[0].init.headers as Record<string, string>;

        expect(headers["content-type"]).toBeUndefined();
    });

    test("unit: response headers arrive with lowercase names", async () => {
        // The conditional request handling in the next phase reads the tag by
        // name, so the casing the server chose must not matter.
        const fetcher = recordingFetch(jsonReply(200, {}));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        const response = await transport.send({ method: "GET", path: "/api/v1/classes" });

        expect(response.headers.etag).toBe('"tag-200"');
    });

    test("edge: a 204 is read as no body rather than as a parse failure", async () => {
        const fetcher = recordingFetch(new Response(null, { status: 204 }));
        const transport = createFetchTransport("http://api.test", fetcher.send);

        const response = await transport.send({ method: "POST", path: "/api/v1/auth/logout" });

        expect(response.status).toBe(204);
        expect(response.body).toBeUndefined();
    });

    test("edge: a non-json failure body does not throw", async () => {
        const fetcher = recordingFetch(
            new Response("<html>Bad Gateway</html>", {
                status: 502,
                headers: { "content-type": "text/html" },
            }),
        );

        const transport = createFetchTransport("http://api.test", fetcher.send);
        const response = await transport.send({ method: "GET", path: "/api/v1/classes" });

        expect(response.status).toBe(502);
        expect(response.body).toBeUndefined();
    });

    test("edge: json that is announced but malformed does not throw", async () => {
        const fetcher = recordingFetch(
            new Response("{not json", { status: 500, headers: { "content-type": "application/json" } }),
        );

        const transport = createFetchTransport("http://api.test", fetcher.send);
        const response = await transport.send({ method: "GET", path: "/api/v1/classes" });

        expect(response.body).toBeUndefined();
    });
});
