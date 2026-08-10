<script lang="ts">
    import { apiBaseUrl } from "$lib/config/environment";
</script>

<section class="placeholder">
    <h1>Trial classes</h1>

    <p>
        The class list is built in phase 4. This page exists so the shell, the routing, and the
        build identity can be proven before any of it depends on the api.
    </p>

    <p class="api-target">api: {apiBaseUrl}</p>
</section>

<style>
    .placeholder {
        max-width: 40rem;
    }

    h1 {
        margin-top: 0;
        font-size: 1.5rem;
    }

    .api-target {
        font-size: 0.85rem;
        color: #6b7280;
    }
</style>
