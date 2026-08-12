import { describe, expect, test } from "vitest";

import { apiBaseUrlForPage, apiBaseUrlForThisPage } from "$lib/config/api_base_url";
import { apiBaseUrl } from "$lib/config/environment";

describe("the api address a page may call", () => {
    test("unit: a page on the same loopback name changes nothing", () => {
        expect(apiBaseUrlForPage("http://127.0.0.1:9000", "127.0.0.1")).toBe(
            "http://127.0.0.1:9000",
        );
    });

    test("behaviour: a page on localhost is given a localhost api", () => {
        // This is the whole reason the module exists. Signing in from
        // localhost:9001 against 127.0.0.1:9000 is cross-site, so the browser
        // discards the session cookies and every later call is a 401.
        expect(apiBaseUrlForPage("http://127.0.0.1:9000", "localhost")).toBe(
            "http://localhost:9000",
        );
    });

    test("behaviour: a page on 127.0.0.1 is given a 127.0.0.1 api", () => {
        expect(apiBaseUrlForPage("http://localhost:9000", "127.0.0.1")).toBe(
            "http://127.0.0.1:9000",
        );
    });

    test("unit: the port survives the rewrite", () => {
        expect(apiBaseUrlForPage("http://127.0.0.1:8080", "localhost")).toBe(
            "http://localhost:8080",
        );
    });

    test("unit: a path on the configured address survives the rewrite", () => {
        expect(apiBaseUrlForPage("http://127.0.0.1:9000/gateway", "localhost")).toBe(
            "http://localhost:9000/gateway",
        );
    });

    test("edge: no trailing slash is ever introduced", () => {
        // The transport concatenates this with a path that starts with a slash.
        expect(apiBaseUrlForPage("http://127.0.0.1:9000", "localhost")).not.toMatch(/\/$/);
    });

    test("edge: a deployed api is never rewritten, whatever the page host", () => {
        expect(apiBaseUrlForPage("https://api.example.test", "localhost")).toBe(
            "https://api.example.test",
        );
        expect(apiBaseUrlForPage("https://api.example.test", "127.0.0.1")).toBe(
            "https://api.example.test",
        );
    });

    test("edge: a page on a real host is never rewritten either", () => {
        expect(apiBaseUrlForPage("http://127.0.0.1:9000", "booking.example.test")).toBe(
            "http://127.0.0.1:9000",
        );
    });

    test("edge: an address that is not a url is handed back untouched", () => {
        expect(apiBaseUrlForPage("not a url", "localhost")).toBe("not a url");
    });

    test("integration: the running page resolves against its own host", () => {
        // jsdom serves the suite from localhost, which is exactly the case the
        // browser refuses cookies for.
        expect(apiBaseUrlForThisPage()).toBe(
            apiBaseUrlForPage(apiBaseUrl, window.location.hostname),
        );
        expect(apiBaseUrlForThisPage()).toMatch(new RegExp(`^https?://${window.location.hostname}`));
    });
});
