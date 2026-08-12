import { render, screen } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import { buildIdentity } from "$lib/config/environment";
import VersionFooter from "./VersionFooter.svelte";

describe("VersionFooter", () => {
    test("unit: it shows the version injected at build time", () => {
        render(VersionFooter);

        expect(screen.getByTestId("version-footer-version")).toHaveTextContent(
            `version ${buildIdentity.version}`,
        );
    });

    test("unit: it shows the commit injected at build time", () => {
        render(VersionFooter);

        const expected = buildIdentity.commit.slice(0, 7);

        expect(screen.getByTestId("version-footer-commit")).toHaveTextContent(`commit ${expected}`);
    });

    test("edge: a long commit is shortened to seven characters", () => {
        // A full hash pushes the footer wide enough to wrap on a narrow screen,
        // which is the one place this text has to stay readable.
        render(VersionFooter);

        const rendered = screen.getByTestId("version-footer-commit").textContent ?? "";
        const shown = rendered.replace("commit ", "");

        expect(shown.length).toBeLessThanOrEqual(7);
    });

    test("edge: the footer carries nothing but version and commit", () => {
        // The sensitive data rule for this surface: it is on screen the whole
        // time, so nothing that identifies a person may reach it.
        render(VersionFooter);

        const rendered = screen.getByTestId("version-footer").textContent ?? "";

        expect(rendered).not.toMatch(/@/);
        expect(rendered).not.toMatch(/token/i);
        expect(rendered).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}/i);
    });
});
