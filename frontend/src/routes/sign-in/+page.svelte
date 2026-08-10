<script lang="ts">
    import { goto } from "$app/navigation";

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
    let submitting = $state(false);
    let failure = $state("");

    const notice = $derived($auth.signedOutReason === null ? "" : noticeForReason[$auth.signedOutReason]);

    async function signIn(event: SubmitEvent): Promise<void> {
        event.preventDefault();

        if (submitting) {
            return;
        }

        submitting = true;
        failure = "";

        try {
            await api.login(email.trim());

            // The api sets two cookies and says nothing else, so who signed in
            // is asked for separately rather than assumed from the form.
            const session = await api.me();

            auth.signIn(session);
            auth.acknowledgeNotice();

            await goto("/");
        } catch (error) {
            failure = error instanceof ApiError ? error.message : "something went wrong, try again";
        } finally {
            submitting = false;
        }
    }
</script>

<section class="sign-in">
    <h1>Sign in</h1>

    <p class="lead">Pick one of the seeded parents. There is no password on this trial service.</p>

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
        color: #6b7280;
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
        border: 1px solid #d1d5db;
        border-radius: 0.25rem;
    }

    button {
        padding: 0.5rem;
        font-size: 1rem;
        font-weight: 600;
        color: #ffffff;
        background: #1d4ed8;
        border: none;
        border-radius: 0.25rem;
        cursor: pointer;
    }

    button:disabled {
        background: #9ca3af;
        cursor: progress;
    }

    .notice {
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: #92400e;
        background: #fef3c7;
        border-radius: 0.25rem;
    }

    .failure {
        font-size: 0.9rem;
        color: #b91c1c;
    }
</style>
