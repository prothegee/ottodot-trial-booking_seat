<script lang="ts">
    import type { Child } from "$lib/api/types";

    interface Props {
        /**
         * The children on this account.
         *
         * Named at length rather than `children`, which Svelte reserves for the
         * content a parent component passes in. Two meanings behind one word in
         * a component file is a bug waiting for somebody in a hurry.
         */
        childrenOnAccount: Child[];

        /** Which child is picked. Bound, so the form owns the value. */
        selected: string;

        disabled?: boolean;
    }

    let { childrenOnAccount, selected = $bindable(), disabled = false }: Props = $props();

    // One child is picked for the parent, because a form with a single option
    // and nothing selected is a click that teaches nobody anything.
    $effect(() => {
        if (selected === "" && childrenOnAccount.length === 1) {
            selected = childrenOnAccount[0].id;
        }
    });
</script>

{#if childrenOnAccount.length === 0}
    <p class="empty" data-testid="child-picker-empty">
        There is no child on this account yet, so there is nobody to book a place for.
    </p>
{:else}
    <fieldset class="child-picker" {disabled} data-testid="child-picker">
        <legend>Who is this for</legend>

        {#each childrenOnAccount as child (child.id)}
            <label class="child">
                <input
                    type="radio"
                    name="student"
                    value={child.id}
                    bind:group={selected}
                    data-testid="child-option"
                    data-student-id={child.id}
                />

                <span class="name">{child.full_name}</span>
                <span class="grade">grade {child.grade_level}</span>
            </label>
        {/each}
    </fieldset>
{/if}

<style>
    .child-picker {
        display: flex;
        flex-direction: column;
        gap: 0.5rem;
        padding: 0.75rem;
        border: 1px solid #e5e7eb;
        border-radius: 0.5rem;
    }

    legend {
        padding: 0 0.35rem;
        font-size: 0.85rem;
        font-weight: 600;
    }

    .child {
        display: flex;
        align-items: baseline;
        gap: 0.5rem;
        font-size: 0.95rem;
    }

    .grade {
        font-size: 0.8rem;
        color: #6b7280;
    }

    .empty {
        font-size: 0.9rem;
        color: #6b7280;
    }
</style>
