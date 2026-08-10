import { render, screen } from "@testing-library/svelte";
import { createRawSnippet } from "svelte";
import { describe, expect, test } from "vitest";

import { buildIdentity } from "$lib/config/environment";
import Layout from "./+layout.svelte";
import { prerender, ssr } from "./+layout";

/** Stands in for whichever page the router happens to have mounted. */
const pageContent = createRawSnippet(() => ({
    render: () => `<p data-testid="page-content">a page</p>`,
}));

describe("the application shell", () => {
    test("integration: it boots and renders header, page, and footer together", () => {
        render(Layout, { props: { children: pageContent } });

        expect(screen.getByRole("banner")).toBeInTheDocument();
        expect(screen.getByRole("main")).toBeInTheDocument();
        expect(screen.getByTestId("page-content")).toBeInTheDocument();
        expect(screen.getByTestId("version-footer")).toBeInTheDocument();
    });

    test("integration: the footer inside the shell shows the injected version", () => {
        render(Layout, { props: { children: pageContent } });

        expect(screen.getByTestId("version-footer-version")).toHaveTextContent(
            `version ${buildIdentity.version}`,
        );
    });

    test("unit: the title links back to the class list", () => {
        render(Layout, { props: { children: pageContent } });

        expect(screen.getByRole("link", { name: "Trial Class Booking" })).toHaveAttribute(
            "href",
            "/",
        );
    });

    test("unit: server rendering and prerendering are both off", () => {
        // This is a decision, not a default. A future page that quietly turns
        // either one back on would start rendering seat counts on a server,
        // which is the one thing this client must never treat as truth.
        expect(ssr).toBe(false);
        expect(prerender).toBe(false);
    });
});
