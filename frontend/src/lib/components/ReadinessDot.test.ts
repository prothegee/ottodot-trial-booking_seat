import { render, screen } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import ReadinessDot from "$lib/components/ReadinessDot.svelte";

describe("the readiness dot", () => {
    test("unit: each of the three states the api reports has its own wording", () => {
        for (const [status, label] of [
            ["ready", "Ready"],
            ["degraded", "Degraded"],
            ["unavailable", "Not ready"],
        ] as const) {
            const shown = render(ReadinessDot, { props: { status } });

            expect(screen.getByTestId("readiness-dot").textContent).toContain(label);

            shown.unmount();
        }
    });

    test("edge: no answer at all is a fourth state, not one of the three", () => {
        // A service that is not talking and a service telling the truth about
        // being broken are different facts, and a dot that showed the same thing
        // for both would be lying about one of them.
        render(ReadinessDot, { props: { status: null } });

        expect(screen.getByTestId("readiness-dot").getAttribute("data-status")).toBe("unknown");
        expect(screen.getByTestId("readiness-dot").textContent).toContain("No answer");
    });

    test("behaviour: the state is readable without seeing the colour", () => {
        // Three states carried by a coloured dot alone would say nothing to
        // somebody who cannot see it. The label is the answer and the dot is the
        // decoration, which is why the dot is hidden from assistive technology.
        const { container } = render(ReadinessDot, { props: { status: "degraded" } });

        const dot = container.querySelector(".dot");

        expect(dot?.getAttribute("aria-hidden")).toBe("true");
        expect(screen.getByTestId("readiness-dot").textContent?.trim()).toBe("Degraded");
    });

    test("edge: an unrecognised state falls back rather than rendering nothing", () => {
        // The api's status set is closed, so this only happens if a backend
        // grows a fourth. A blank dot would be the worst possible answer to
        // that.
        render(ReadinessDot, { props: { status: "something_new" as never } });

        expect(screen.getByTestId("readiness-dot").textContent).toContain("No answer");
    });
});
