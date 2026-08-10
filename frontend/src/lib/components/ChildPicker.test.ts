import { render, screen } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import type { Child } from "$lib/api/types";
import ChildPicker from "./ChildPicker.svelte";

const mira: Child = { id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 };
const arun: Child = { id: "0192a000-0000-7000-8000-000000000012", full_name: "Arun Tan", grade_level: 3 };

describe("the child picker", () => {
    test("integration: every child on the account is offered", () => {
        render(ChildPicker, { props: { childrenOnAccount: [mira, arun], selected: "" } });

        expect(screen.getAllByTestId("child-option")).toHaveLength(2);
        expect(screen.getByText("Mira Tan")).toBeInTheDocument();
        expect(screen.getByText("Arun Tan")).toBeInTheDocument();
    });

    test("unit: the grade is shown beside the name, so two children are told apart", () => {
        render(ChildPicker, { props: { childrenOnAccount: [mira, arun], selected: "" } });

        expect(screen.getByText("grade 5")).toBeInTheDocument();
        expect(screen.getByText("grade 3")).toBeInTheDocument();
    });

    test("edge: an account with one child has that child picked already", async () => {
        // A form with a single option and nothing selected is a click that
        // teaches nobody anything.
        render(ChildPicker, { props: { childrenOnAccount: [mira], selected: "" } });

        const option = (await screen.findByTestId("child-option")) as HTMLInputElement;

        expect(option.checked).toBe(true);
    });

    test("edge: an account with two children picks neither, so the parent chooses", async () => {
        render(ChildPicker, { props: { childrenOnAccount: [mira, arun], selected: "" } });

        const options = screen.getAllByTestId("child-option") as HTMLInputElement[];

        expect(options.some((option) => option.checked)).toBe(false);
    });

    test("edge: an account with no children says so instead of showing an empty box", () => {
        render(ChildPicker, { props: { childrenOnAccount: [], selected: "" } });

        expect(screen.getByTestId("child-picker-empty")).toBeInTheDocument();
        expect(screen.queryByTestId("child-picker")).not.toBeInTheDocument();
    });

    test("edge: the picker is disabled while a request is in flight", () => {
        // Without it, a parent can change the child under a submit that has
        // already been sent, and the screen would show one answer for another
        // question.
        render(ChildPicker, { props: { childrenOnAccount: [mira, arun], selected: mira.id, disabled: true } });

        expect(screen.getByTestId("child-picker")).toBeDisabled();
    });
});
