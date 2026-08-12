import { render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, expect, test } from "vitest";

import PasswordField from "$lib/components/PasswordField.svelte";

/** Renders the field the way the sign in form uses it. */
function shownField(): { input: HTMLInputElement; reveal: HTMLButtonElement } {
    render(PasswordField, { props: { id: "sign-in-password", value: "", testId: "password" } });

    return {
        input: screen.getByTestId("password") as HTMLInputElement,
        reveal: screen.getByTestId("password-reveal") as HTMLButtonElement,
    };
}

/** Types into the field the way a browser does, through an input event. */
function type(input: HTMLInputElement, value: string): void {
    input.value = value;
    input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("the password field", () => {
    test("unit: what is typed is hidden until it is asked for", () => {
        // Hidden is the state somebody standing behind a desk expects, so it is
        // the one the screen opens in.
        const { input } = shownField();

        expect(input.type).toBe("password");
    });

    test("behaviour: pressing the control shows the characters", async () => {
        const { input, reveal } = shownField();

        reveal.click();
        await tick();

        expect(input.type).toBe("text");
    });

    test("behaviour: pressing it again hides them", async () => {
        const { input, reveal } = shownField();

        reveal.click();
        await tick();

        reveal.click();
        await tick();

        expect(input.type).toBe("password");
    });

    test("integration: revealing keeps what was already typed", async () => {
        // One input whose type changes rather than two swapped by a block. Two
        // would lose the value, the caret, and the focus on every press.
        const { input, reveal } = shownField();

        type(input, "otto123");
        reveal.click();
        await tick();

        expect(input.value).toBe("otto123");
    });

    test("edge: the control does not submit the form it sits in", () => {
        // A button inside a form submits by default, so revealing a password
        // would send a half filled sign in.
        const { reveal } = shownField();

        expect(reveal.type).toBe("button");
    });

    test("behaviour: the control says what it does, and says which state it is in", async () => {
        // The icon carries no words, so the label is the whole answer for
        // somebody who cannot see it.
        const { reveal } = shownField();

        expect(reveal.getAttribute("aria-label")).toBe("Show password");
        expect(reveal.getAttribute("aria-pressed")).toBe("false");

        reveal.click();
        await tick();

        expect(reveal.getAttribute("aria-label")).toBe("Hide password");
        expect(reveal.getAttribute("aria-pressed")).toBe("true");
    });

    test("unit: the control points at the field it belongs to", () => {
        const { input, reveal } = shownField();

        expect(reveal.getAttribute("aria-controls")).toBe(input.id);
    });

    test("unit: the icon is decoration and not something to read out", () => {
        const { reveal } = shownField();

        const icon = reveal.querySelector("svg");

        expect(icon?.getAttribute("aria-hidden")).toBe("true");
    });

    test("integration: a password manager is told which password this is", () => {
        // current-password offers the stored one. new-password would offer to
        // save whatever was typed, on every visit to the sign in screen.
        const { input } = shownField();

        expect(input.getAttribute("autocomplete")).toBe("current-password");
        expect(input.getAttribute("name")).toBe("password");
    });

    test("edge: a field asked to be optional does not demand a value", () => {
        render(PasswordField, { props: { id: "optional-password", value: "", testId: "optional" } });

        expect((screen.getByTestId("optional") as HTMLInputElement).required).toBe(false);
    });
});
