import { render, screen, waitFor } from "@testing-library/svelte";
import { createRawSnippet } from "svelte";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { get } from "svelte/store";

import { buildIdentity } from "$lib/config/environment";
import { restoreSession } from "$lib/session/restore";
import { requestedSignOut } from "$lib/session/sign_out_request";
import { auth } from "$lib/stores/auth";
import type { Session } from "$lib/api/types";
import Layout from "./+layout.svelte";
import { prerender, ssr } from "./+layout";

// The sign out is mocked because what is under test here is the control: that
// the header offers it, only to somebody signed in, and only once per press.
// What the sign out itself does is sign_out_request.test.ts.
vi.mock("$lib/session/sign_out_request", () => ({
    requestedSignOut: vi.fn(() => Promise.resolve()),
}));

// Mocked for the same reason. What restoring does is restore.test.ts, and what
// belongs here is that the shell asks at all.
vi.mock("$lib/session/restore", () => ({
    restoreSession: vi.fn(() => Promise.resolve(false)),
}));

/** Stands in for whichever page the router happens to have mounted. */
const pageContent = createRawSnippet(() => ({
    render: () => `<p data-testid="page-content">a page</p>`,
}));

const seededSession: Session = {
    parent_id: "0192a000-0000-7000-8000-000000000001",
    display_name: "Ada Tan",
    role: "parent",
    children: [],
};

describe("the application shell", () => {
    beforeEach(() => {
        vi.mocked(requestedSignOut).mockClear();
        vi.mocked(restoreSession).mockClear().mockResolvedValue(false);
        auth.signOut("requested");
        auth.acknowledgeNotice();
    });

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

    test("unit: the header carries the two parent links and nothing operational", () => {
        // The header is for a parent booking a seat. An operations screen
        // advertised on every page of it is one wrong click away from a reviewer
        // wondering whether it is part of the booking flow.
        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        const header = screen.getByRole("banner");

        expect(header.querySelectorAll("a")).toHaveLength(2);
        expect(header.querySelector('a[href="/status"]')).toBeNull();
    });

    test("integration: somebody signed in is offered the way back to their bookings", () => {
        // Every link to a booking sits on the payment screen. Without this one,
        // a parent who closed the tab after paying has no way back to it.
        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        expect(screen.getByTestId("my-bookings")).toHaveAttribute("href", "/bookings");
    });

    test("edge: nobody signed in is not offered a list that would be empty", () => {
        render(Layout, { props: { children: pageContent } });

        expect(screen.queryByTestId("my-bookings")).not.toBeInTheDocument();
    });

    test("integration: somebody signed in is shown who they are and a way out", () => {
        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        expect(screen.getByTestId("signed-in-as")).toHaveTextContent("Ada Tan");
        expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
    });

    test("integration: pressing it ends the session", async () => {
        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        screen.getByTestId("sign-out").click();

        expect(vi.mocked(requestedSignOut)).toHaveBeenCalledTimes(1);
    });

    test("unit: the control is a button and not a link", () => {
        // It ends a session, which is a write. A link would be followed by a
        // prefetch, by a crawler, and by the browser restoring a tab.
        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        expect(screen.getByTestId("sign-out").tagName).toBe("BUTTON");
    });

    test("edge: nobody signed in is offered nothing to sign out of", () => {
        // The sign-in screen renders inside this same shell.
        render(Layout, { props: { children: pageContent } });

        expect(screen.queryByTestId("sign-out")).not.toBeInTheDocument();
        expect(screen.queryByTestId("signed-in-as")).not.toBeInTheDocument();
    });

    test("edge: a second press while the first is running is ignored", async () => {
        // Two logouts race the same refresh token, and the api answers the
        // second one as a reuse. That refusal is correct and would reach the
        // parent as the notice meant for a stolen session.
        let releaseFirst = (): void => {};

        vi.mocked(requestedSignOut).mockImplementationOnce(
            () =>
                new Promise<void>((resolve) => {
                    releaseFirst = resolve;
                }),
        );

        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        const control = screen.getByTestId("sign-out");

        control.click();
        await waitFor(() => expect(control).toBeDisabled());
        control.click();

        expect(vi.mocked(requestedSignOut)).toHaveBeenCalledTimes(1);

        releaseFirst();
    });

    test("edge: the control says what it is doing while it runs", async () => {
        let releaseFirst = (): void => {};

        vi.mocked(requestedSignOut).mockImplementationOnce(
            () =>
                new Promise<void>((resolve) => {
                    releaseFirst = resolve;
                }),
        );

        auth.signIn(seededSession);

        render(Layout, { props: { children: pageContent } });

        screen.getByTestId("sign-out").click();

        await waitFor(() => expect(screen.getByTestId("sign-out")).toHaveTextContent("Signing out"));

        releaseFirst();
    });

    test("edge: the session the store holds is what decides, not the route", () => {
        // The shell has no idea which page is mounted, and it must not need one.
        auth.signIn(seededSession);

        const { unmount } = render(Layout, { props: { children: pageContent } });

        expect(get(auth).session).not.toBeNull();
        expect(screen.getByTestId("sign-out")).toBeInTheDocument();

        unmount();
    });

    test("integration: the shell asks who the cookies belong to when it loads", async () => {
        // Without it, a signed-in parent opening a second tab is a stranger to
        // this client: no way to their bookings, and a sign-in screen offering
        // them a form they have already filled in.
        render(Layout, { props: { children: pageContent } });

        await waitFor(() => expect(vi.mocked(restoreSession)).toHaveBeenCalledTimes(1));
    });

    test("edge: it asks once, not on every change to the session", async () => {
        render(Layout, { props: { children: pageContent } });

        await waitFor(() => expect(vi.mocked(restoreSession)).toHaveBeenCalledTimes(1));

        auth.signIn(seededSession);

        await waitFor(() => expect(screen.getByTestId("signed-in-as")).toBeInTheDocument());

        expect(vi.mocked(restoreSession)).toHaveBeenCalledTimes(1);
    });

    test("unit: server rendering and prerendering are both off", () => {
        // This is a decision, not a default. A future page that quietly turns
        // either one back on would start rendering seat counts on a server,
        // which is the one thing this client must never treat as truth.
        expect(ssr).toBe(false);
        expect(prerender).toBe(false);
    });
});
