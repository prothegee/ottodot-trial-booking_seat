<script lang="ts">
    interface Props {
        /** The input's id, so a label outside this component can point at it. */
        id: string;

        /** What was typed. Bound, so the form owns the value. */
        value: string;

        name?: string;

        /**
         * current-password where an existing one is being typed, new-password
         * where one is being chosen. It decides whether a password manager
         * offers the stored password or offers to save what was just typed.
         */
        autocomplete?: "current-password" | "new-password";

        required?: boolean;

        /** What a test reaches for. The reveal control is this with `-reveal` after it. */
        testId?: string;
    }

    let {
        id,
        value = $bindable(),
        name = "password",
        autocomplete = "current-password",
        required = false,
        testId = "password",
    }: Props = $props();

    /**
     * Whether the characters are on screen.
     *
     * It starts hidden and is never remembered between visits. Somebody who
     * revealed their password once at a desk did not agree to reveal it on the
     * next screen they open.
     */
    let revealed = $state(false);

    const revealLabel = $derived(revealed ? "Hide password" : "Show password");
</script>

<div class="field">
    <!--
        The type is read rather than bound, because Svelte refuses bind:value on
        an input whose type changes, and the value is written back by hand
        instead. That keeps this one element rather than two swapped by a block,
        so revealing the password does not take the focus or move the caret.
    -->
    <input
        {id}
        {name}
        {autocomplete}
        {required}
        {value}
        type={revealed ? "text" : "password"}
        oninput={(event) => (value = event.currentTarget.value)}
        data-testid={testId}
    />

    <button
        type="button"
        class="reveal"
        aria-controls={id}
        aria-pressed={revealed}
        aria-label={revealLabel}
        title={revealLabel}
        onclick={() => (revealed = !revealed)}
        data-testid={`${testId}-reveal`}
    >
        <!--
            The icon is drawn here rather than loaded, so the control works on a
            machine with no network and nothing to wait for. It is hidden from a
            screen reader because the button already says what it does.
        -->
        {#if revealed}
            <svg
                viewBox="0 0 24 24"
                width="18"
                height="18"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                focusable="false"
            >
                <path
                    d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"
                />
                <line x1="1" y1="1" x2="23" y2="23" />
            </svg>
        {:else}
            <svg
                viewBox="0 0 24 24"
                width="18"
                height="18"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                focusable="false"
            >
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                <circle cx="12" cy="12" r="3" />
            </svg>
        {/if}
    </button>
</div>

<style>
    .field {
        position: relative;
        display: flex;
    }

    /*
        The same shape as every other field on the form. It is repeated here
        rather than inherited, because a page's styles are scoped to the page
        and do not reach into a component.
    */
    input {
        flex: 1;
        min-width: 0;
        padding: 0.5rem;
        padding-right: 2.75rem;
        font-size: 1rem;
        border: 1px solid var(--line-strong);
        border-radius: 0.25rem;
    }

    .reveal {
        position: absolute;
        top: 0;
        right: 0;
        bottom: 0;
        display: flex;
        align-items: center;
        padding: 0 0.7rem;
        color: var(--muted);
        background: none;
        border: none;
        border-radius: 0 0.25rem 0.25rem 0;
        cursor: pointer;
    }

    .reveal:hover {
        color: var(--ink-soft);
    }
</style>
