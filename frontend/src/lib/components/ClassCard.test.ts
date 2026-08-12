import { render, screen } from "@testing-library/svelte";
import { describe, expect, test } from "vitest";

import type { TrialClass } from "$lib/api/types";
import ClassCard from "./ClassCard.svelte";

/** A class with room, which every case here starts from and then narrows. */
function openClass(overrides: Partial<TrialClass> = {}): TrialClass {
    return {
        id: "0192a000-0000-7000-8000-000000000021",
        subject: "science",
        title: "Science Discovery",
        starts_at: "2026-08-14T09:00:00Z",
        duration_minutes: 60,
        capacity: 4,
        seats_remaining: 3,
        ...overrides,
    };
}

describe("a class card", () => {
    test("integration: a class with room shows its count and a way to book", () => {
        render(ClassCard, { props: { trialClass: openClass() } });

        expect(screen.getByTestId("class-card-seats")).toHaveTextContent("3 of 4 seats left");
        expect(screen.getByTestId("class-card-book")).toHaveAttribute(
            "href",
            "/book/0192a000-0000-7000-8000-000000000021",
        );
    });

    test("unit: the subject, the title, and the length are all on the card", () => {
        render(ClassCard, { props: { trialClass: openClass() } });

        expect(screen.getByText("science")).toBeInTheDocument();
        expect(screen.getByRole("heading", { name: "Science Discovery" })).toBeInTheDocument();
        expect(screen.getByText(/60 minutes/)).toBeInTheDocument();
    });

    test("edge: zero seats renders as full", () => {
        render(ClassCard, { props: { trialClass: openClass({ seats_remaining: 0 }) } });

        expect(screen.getByTestId("class-card-seats")).toHaveTextContent("Full");
        expect(screen.queryByTestId("class-card-book")).not.toBeInTheDocument();
    });

    test("edge: capacity equal to the seats confirmed renders as full", () => {
        // The api has already done the subtraction, so this arrives as the same
        // zero. Both cases are asserted because both are named in the plan and
        // a later change could tell them apart again.
        render(ClassCard, { props: { trialClass: openClass({ capacity: 4, seats_remaining: 0 }) } });

        expect(screen.getByTestId("class-card-seats")).toHaveTextContent("Full");
    });

    test("edge: a negative count reads as full rather than as a nonsense number", () => {
        // It can only arrive if capacity were lowered under confirmed bookings,
        // which is exactly the kind of data change an operator makes at some
        // point. Rendering "-1 of 4 seats left" would be the worse answer.
        render(ClassCard, { props: { trialClass: openClass({ seats_remaining: -1 }) } });

        expect(screen.getByTestId("class-card-seats")).toHaveTextContent("Full");
        expect(screen.queryByTestId("class-card-book")).not.toBeInTheDocument();
    });

    test("edge: a full class says why there is no button, rather than showing a dead one", () => {
        render(ClassCard, { props: { trialClass: openClass({ seats_remaining: 0 }) } });

        expect(screen.getByTestId("class-card-closed")).toBeInTheDocument();
    });

    test("unit: everything a reader can act on is the last block in the card", () => {
        // The bottom alignment hangs on this. The card is a column that the grid
        // stretches to the tallest one in the row, and the spare height is sent
        // above this block, so a button that sat loose among the text would go
        // back to being wherever the words above it happened to end.
        const { container } = render(ClassCard, { props: { trialClass: openClass(), showRoster: true } });

        const card = container.querySelector('[data-testid="class-card"]');
        const actions = card?.lastElementChild;

        expect(actions).not.toBeNull();
        expect(actions?.querySelector('[data-testid="class-card-book"]')).not.toBeNull();
        expect(actions?.querySelector('[data-testid="class-card-roster"]')).not.toBeNull();
    });

    test("edge: a full class puts its note in the same block the button would have used", () => {
        // Otherwise a full card and a bookable one would end at different
        // heights, which is the staircase again with one card in it.
        const { container } = render(ClassCard, { props: { trialClass: openClass({ seats_remaining: 0 }) } });

        const actions = container.querySelector('[data-testid="class-card"]')?.lastElementChild;

        expect(actions?.querySelector('[data-testid="class-card-closed"]')).not.toBeNull();
    });

    test("edge: one seat left is still bookable", () => {
        // The boundary the whole exercise is about. Off by one here would hide
        // the last seat from everybody.
        render(ClassCard, { props: { trialClass: openClass({ seats_remaining: 1 }) } });

        expect(screen.getByTestId("class-card-seats")).toHaveTextContent("1 of 4 seats left");
        expect(screen.getByTestId("class-card-book")).toBeInTheDocument();
    });
});
