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

/** The password every seeded account shares, as the seed and how-to.md state it. */
const seededPassword = "otto123";

/** Types one value into a field the way a parent would. */
function type(testId: string, value: string): void {
    const field = screen.getByTestId(testId) as HTMLInputElement;

    field.value = value;
    field.dispatchEvent(new Event("input", { bubbles: true }));
}

/** Fills both fields and submits, the way a parent would. */
async function signInAs(email: string, password = seededPassword): Promise<void> {
    type("sign-in-email", email);
    type("sign-in-password", password);

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
            expect(api.login).toHaveBeenCalledWith("ada@example.test", seededPassword);
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
            expect(api.login).toHaveBeenCalledWith("ada@example.test", seededPassword);
        });
    });

    test("integration: the password is sent as typed, with no trimming", async () => {
        // A space is a character somebody may have chosen. Trimming it would
        // refuse a password that is actually right.
        render(SignInPage);

        await signInAs("ada@example.test", "  spaced out  ");

        await waitFor(() => {
            expect(api.login).toHaveBeenCalledWith("ada@example.test", "  spaced out  ");
        });
    });

    test("behaviour: the password field hides what is typed", async () => {
        render(SignInPage);

        const field = screen.getByTestId("sign-in-password") as HTMLInputElement;

        expect(field.type).toBe("password");
    });

    test("integration: the password can be shown, for somebody checking what they typed", async () => {
        // A hidden field with no way to look is where a wrong password gets
        // typed twice. The control itself is PasswordField's, and this is the
        // case that says the sign in screen actually has one.
        render(SignInPage);

        const field = screen.getByTestId("sign-in-password") as HTMLInputElement;

        type("sign-in-password", seededPassword);
        screen.getByTestId("sign-in-password-reveal").click();

        await waitFor(() => expect(field.type).toBe("text"));

        expect(field.value).toBe(seededPassword);
    });

    test("edge: a refused sign in never says which half was wrong", async () => {
        // The api answers an unknown address and a wrong password with the same
        // refusal on purpose. A screen that guessed between them would hand
        // back the difference the api is hiding.
        vi.mocked(api.login).mockRejectedValue(new ApiError("InvalidRequest", "invalid_request", 400));

        render(SignInPage);

        await signInAs("nobody@example.test");

        const failure = await screen.findByTestId("sign-in-failure");

        expect(failure.textContent).toContain("do not match");
        expect(failure.textContent).not.toContain("password is");
        expect(failure.textContent).not.toContain("no account");
        expect(goto).not.toHaveBeenCalled();
    });

    test("edge: a refused sign in never echoes what was typed", async () => {
        vi.mocked(api.login).mockRejectedValue(new ApiError("InvalidRequest", "invalid_request", 400));

        render(SignInPage);

        await signInAs("nobody@example.test", "hunter2");

        const failure = await screen.findByTestId("sign-in-failure");

        expect(failure.textContent).not.toContain("hunter2");
        expect(failure.textContent).not.toContain("nobody@example.test");
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

    test("behaviour: somebody already signed in is sent away rather than shown the form", async () => {
        // A fresh tab has no memory of the session, which lives in cookies this
        // code cannot read. The shell restores it, and this screen is where a
        // parent would otherwise be asked to sign in to an account they are
        // already signed in to.
        auth.signIn(seededSession);

        render(SignInPage);

        await waitFor(() => expect(goto).toHaveBeenCalledWith("/"));
    });

    test("edge: nobody signed in is left on the form", () => {
        render(SignInPage);

        expect(goto).not.toHaveBeenCalled();
        expect(screen.getByTestId("sign-in-submit")).toBeInTheDocument();
    });
});
