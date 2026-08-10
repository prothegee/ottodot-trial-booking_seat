<script lang="ts">
    import VersionFooter from "$lib/components/VersionFooter.svelte";

    let { children } = $props();
</script>

<div class="shell">
    <header class="shell-header">
        <a class="shell-title" href="/">Trial Class Booking</a>
    </header>

    <main class="shell-main">
        {@render children()}
    </main>

    <VersionFooter />
</div>

<style>
    :global(body) {
        margin: 0;
        font-family:
            system-ui,
            -apple-system,
            "Segoe UI",
            sans-serif;
        color: #111827;
        background: #f9fafb;
    }

    .shell {
        display: flex;
        flex-direction: column;
        min-height: 100vh;
    }

    .shell-header {
        padding: 1rem 1.5rem;
        background: #ffffff;
        border-bottom: 1px solid #e5e7eb;
    }

    .shell-title {
        font-size: 1.1rem;
        font-weight: 600;
        color: #111827;
        text-decoration: none;
    }

    .shell-main {
        flex: 1;
        padding: 1.5rem;
    }
</style>
