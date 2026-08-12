<script lang="ts">
    import type { ReadinessStatus } from "$lib/api/types";

    interface Props {
        /**
         * What the backend last said, or null when it said nothing at all.
         *
         * Null is a fourth state and not the same as unavailable. Unavailable is
         * a service that is talking and telling the truth about being broken.
         * Null is a service that is not talking, and the two want different
         * words on screen.
         */
        status: ReadinessStatus | null;
    }

    let { status }: Props = $props();

    /**
     * The three states the api reports, and the one it cannot.
     *
     * Amber is the one worth explaining. A degraded service is correct and is
     * still deciding every seat from the primary, and the only thing behind it
     * is a replica that has fallen behind or over. Colouring that red would send
     * somebody looking for a problem that is not costing anybody a booking.
     */
    const wording: Record<string, { label: string; tone: string }> = {
        ready: { label: "Ready", tone: "green" },
        degraded: { label: "Degraded", tone: "amber" },
        unavailable: { label: "Not ready", tone: "red" },
        unknown: { label: "No answer", tone: "grey" },
    };

    let shown = $derived(wording[status ?? "unknown"] ?? wording.unknown);
</script>

<span class="readiness" data-testid="readiness-dot" data-status={status ?? "unknown"}>
    <span class="dot" data-tone={shown.tone} aria-hidden="true"></span>

    <!--
        The label is the accessible answer, not the colour. A dot alone would
        say nothing at all to somebody who cannot see it, and the three states
        are exactly the kind of thing a colour is the worst way to carry.
    -->
    <span class="label">{shown.label}</span>
</span>

<style>
    .readiness {
        display: inline-flex;
        align-items: center;
        gap: 0.5rem;
        font-weight: 600;
    }

    .dot {
        width: 0.75rem;
        height: 0.75rem;
        border-radius: 50%;
        background: var(--muted-soft);
    }

    .dot[data-tone="green"] {
        background: var(--good-strong);
    }

    .dot[data-tone="amber"] {
        background: var(--warn-strong);
    }

    .dot[data-tone="red"] {
        background: var(--danger);
    }

    .label {
        font-size: 0.95rem;
    }
</style>
