import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { goto } from "$app/navigation";
import { api } from "$lib/session/client";
import { auth } from "$lib/stores/auth";
import { ApiError } from "$lib/api/errors";
import type { Session } from "$lib/api/types";
import SignInPage from "./+page.svelte";

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

// The wired client is replaced so this test is about the screen, not about the
// network. The client itself is covered by its own tests.
vi.mock("$lib/session/client", () => ({
    api: {
        login: vi.fn(() => Promise.resolve()),
        me: vi.fn(() => Promise.resolve()),
        logout: vi.fn(() => Promise.resolve()),
        request: vi.fn(() => Promise.resolve()),
    },
}));

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [{ id: "0192a000-0000-7000-8000-000000000011", full_name: "Mira Tan", grade_level: 5 }],
};

/** Fills the email field and submits, the way a parent would. */
async function signInAs(email: string): Promise<void> {
    const field = screen.getByTestId("sign-in-email") as HTMLInputElement;

    field.value = email;
    field.dispatchEvent(new Event("input", { bubbles: true }));

    screen.getByTestId("sign-in-submit").click();
}

describe("the sign-in screen", () => {
    beforeEach(() => {
        vi.mocked(goto).mockClear();
        vi.mocked(api.login).mockClear().mockResolvedValue(undefined);
        vi.mocked(api.me).mockClear().mockResolvedValue(seededSession);

        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

    test("integration: signing in stores the session and moves on to the class list", async () => {
        render(SignInPage);

        await signInAs("ada@example.test");

        await waitFor(() => {
            expect(api.login).toHaveBeenCalledWith("ada@example.test");
        });

        await waitFor(() => {
            expect(get(auth).session?.display_name).toBe("Ada Tan");
            expect(goto).toHaveBeenCalledWith("/");
        });
    });

    test("edge: surrounding spaces in the email are trimmed before it is sent", async () => {
        render(SignInPage);

        await signInAs("  ada@example.test  ");

        await waitFor(() => {
            expect(api.login).toHaveBeenCalledWith("ada@example.test");
        });
    });

    test("edge: a refused sign in is shown in this client's own words", async () => {
        vi.mocked(api.login).mockRejectedValue(new ApiError("InvalidRequest", "invalid_request", 400));

        render(SignInPage);

        await signInAs("nobody@example.test");

        const failure = await screen.findByTestId("sign-in-failure");

        expect(failure.textContent).toContain("check it and try again");
        expect(goto).not.toHaveBeenCalled();
    });

    test("edge: a failure that is not from the api still leaves a readable message", async () => {
        vi.mocked(api.login).mockRejectedValue(new TypeError("network down"));

        render(SignInPage);

        await signInAs("ada@example.test");

        const failure = await screen.findByTestId("sign-in-failure");

        expect(failure.textContent).toContain("something went wrong");
        expect(failure.textContent).not.toContain("network down");
    });

    test("behaviour: a parent signed out by a reused token is told why", () => {
        // The other half of simulation F10. The reason is put in the store by
        // the hard sign out, and this is the screen that has to state it.
        auth.signOut("token_reused");

        render(SignInPage);

        expect(screen.getByTestId("sign-in-notice").textContent).toContain("used somewhere else");
    });

    test("edge: a parent who signed out deliberately gets no notice", () => {
        auth.signOut("requested");

        render(SignInPage);

        expect(screen.queryByTestId("sign-in-notice")).toBeNull();
    });

    test("edge: arriving fresh with no reason shows no notice", () => {
        render(SignInPage);

        expect(screen.queryByTestId("sign-in-notice")).toBeNull();
    });
});
