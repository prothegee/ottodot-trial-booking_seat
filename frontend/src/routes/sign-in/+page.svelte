<script lang="ts">
    import { goto } from "$app/navigation";

    import PasswordField from "$lib/components/PasswordField.svelte";
    import { ApiError } from "$lib/api/errors";
    import { api } from "$lib/session/client";
    import { auth, type SignOutReason } from "$lib/stores/auth";

    /**
     * Why the parent is here, when they did not choose to be.
     *
     * The wording belongs to this client, not to the api. A parent whose
     * session was taken over is told plainly, because a silent return to the
     * form looks like a bug and hides something they should know about.
     */
    const noticeForReason: Record<SignOutReason, string> = {
        requested: "",
        session_ended: "your session ended, sign in again",
        token_reused: "you were signed out because that session was used somewhere else, sign in again",
    };

    let email = $state("");
    let password = $state("");
    let submitting = $state(false);
    let failure = $state("");

    const notice = $derived($auth.signedOutReason === null ? "" : noticeForReason[$auth.signedOutReason]);

    // Somebody already signed in has nothing to do on this screen. It covers
    // both ways of arriving: a parent who typed the address, and a fresh tab
    // whose session the shell has just restored from the cookies.
    //
    // It is also what moves a successful sign in along, so there is one place
    // that decides where a signed-in parent belongs rather than two.
    $effect(() => {
        if ($auth.session !== null) {
            void goto("/");
        }
    });

    /**
     * What a refused sign in says.
     *
     * The api answers an unknown address and a wrong password with the same
     * refusal on purpose, so this screen must not guess which one happened. One
     * message covering both is what keeps that property, and it is also the
     * honest thing to show: this screen genuinely does not know.
     */
    function signInFailure(error: unknown): string {
        if (!(error instanceof ApiError)) {
            return "something went wrong, try again";
        }

        if (error.kind === "InvalidRequest") {
            return "that email and password do not match an account";
        }

        return error.message;
    }

    async function signIn(event: SubmitEvent): Promise<void> {
        event.preventDefault();

        if (submitting) {
            return;
        }

        submitting = true;
        failure = "";

        try {
            // The email is trimmed and the password is not. A space is a
            // character somebody may have chosen, and silently removing it would
            // refuse a password that is actually right.
            await api.login(email.trim(), password);

            // The api sets two cookies and says nothing else, so who signed in
            // is asked for separately rather than assumed from the form.
            const session = await api.me();

            // Recording the session is what moves the screen on. The effect
            // above watches for it, so a parent who was already signed in and a
            // parent who just signed in end up in the same place by the same
            // line of code.
            auth.signIn(session);
            auth.acknowledgeNotice();
        } catch (error) {
            failure = signInFailure(error);
        } finally {
            submitting = false;
        }
    }
</script>

<section class="sign-in">
    <h1>Sign in</h1>

    <p class="lead">Sign in with one of the seeded accounts. They all share one password.</p>

    {#if notice !== ""}
        <p class="notice" role="status" data-testid="sign-in-notice">{notice}</p>
    {/if}

    <form onsubmit={signIn}>
        <label for="sign-in-email">Email</label>

        <input
            id="sign-in-email"
            name="email"
            type="email"
            autocomplete="email"
            required
            bind:value={email}
            data-testid="sign-in-email"
        />

        <label for="sign-in-password">Password</label>

        <!--
            autocomplete is current-password so a password manager offers the
            stored one rather than offering to save a new one on every visit.
        -->
        <PasswordField
            id="sign-in-password"
            autocomplete="current-password"
            required
            bind:value={password}
            testId="sign-in-password"
        />

        <button type="submit" disabled={submitting} data-testid="sign-in-submit">
            {submitting ? "Signing in" : "Sign in"}
        </button>
    </form>

    {#if failure !== ""}
        <p class="failure" role="alert" data-testid="sign-in-failure">{failure}</p>
    {/if}
</section>

<style>
    .sign-in {
        max-width: 24rem;
    }

    h1 {
        margin-top: 0;
        font-size: 1.5rem;
    }

    .lead {
        color: var(--muted);
        font-size: 0.9rem;
    }

    form {
        display: flex;
        flex-direction: column;
        gap: 0.5rem;
    }

    label {
        font-size: 0.85rem;
        font-weight: 600;
    }

    input {
        padding: 0.5rem;
        font-size: 1rem;
        border: 1px solid var(--line-strong);
        border-radius: 0.25rem;
    }

    button {
        padding: 0.5rem;
        font-size: 1rem;
        font-weight: 600;
        color: var(--on-accent);
        background: var(--accent);
        border: none;
        border-radius: 0.25rem;
        cursor: pointer;
    }

    button:disabled {
        background: var(--muted-soft);
        cursor: progress;
    }

    .notice {
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: var(--warn);
        background: var(--warn-surface);
        border-radius: 0.25rem;
    }

    /*
        The same banner every other screen gives a refusal. It was a bare red
        line here, which is the one place in this client where a failure could
        be mistaken for a hint under the field, and this is the screen where
        somebody is most likely to be stuck.
    */
    .failure {
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: var(--danger);
        background: var(--danger-surface);
        border-radius: 0.25rem;
    }
</style>
