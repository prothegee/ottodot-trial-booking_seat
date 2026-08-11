import { render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, test, vi } from "vitest";

import { mockCaptchaToken } from "$lib/booking/bot_signals";
import CaptchaWidget from "./CaptchaWidget.svelte";

afterEach(() => {
    vi.useRealTimers();
});

describe("the challenge widget", () => {
    test("integration: it hands back one token once it has solved", async () => {
        vi.useFakeTimers();

        const tokens: string[] = [];

        render(CaptchaWidget, { props: { onToken: (token: string) => tokens.push(token) } });

        await vi.advanceTimersByTimeAsync(1000);

        expect(tokens).toEqual([mockCaptchaToken]);
    });

    test("behaviour: it has not solved on mount, which is a state a screen has to handle", () => {
        vi.useFakeTimers();

        const tokens: string[] = [];

        render(CaptchaWidget, { props: { onToken: (token: string) => tokens.push(token) } });

        expect(tokens).toHaveLength(0);
        expect(screen.getByTestId("captcha-widget")).toHaveAttribute("data-solved", "false");
    });

    test("behaviour: it reports having solved, so the state is visible to a person", async () => {
        vi.useFakeTimers();

        render(CaptchaWidget, { props: { onToken: () => {} } });

        await vi.advanceTimersByTimeAsync(1000);

        expect(screen.getByTestId("captcha-widget")).toHaveAttribute("data-solved", "true");
    });

    test("edge: it says plainly that no third party is contacted", () => {
        // A mock challenge that looked like a real one would be worse than one
        // that says what it is.
        render(CaptchaWidget, { props: { onToken: () => {} } });

        expect(screen.getByText(/mock challenge/i)).toBeInTheDocument();
    });

    test("edge: a widget that is taken off screen does not call back afterwards", async () => {
        vi.useFakeTimers();

        const tokens: string[] = [];

        const { unmount } = render(CaptchaWidget, {
            props: { onToken: (token: string) => tokens.push(token) },
        });

        unmount();

        await vi.advanceTimersByTimeAsync(1000);

        expect(tokens).toHaveLength(0);
    });
});
