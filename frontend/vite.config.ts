import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

/**
 * Three values reach the bundle from the environment, and none of them is ever
 * written in a component.
 *
 * API_BASE_URL: where the client sends every call. Hardcoding it in a component
 * would mean a rebuild is the only way to point at a different backend, and it
 * would mean finding every copy.
 *
 * BUILD_VERSION and BUILD_COMMIT: what the footer and the /status route show.
 * A reviewer watching a recording has to be able to tell which build is on
 * screen.
 *
 * They are injected as compile time constants rather than read at runtime, so
 * nothing has to be fetched before the first paint.
 */
const apiBaseUrl = process.env.API_BASE_URL ?? "http://127.0.0.1:9000";
const buildVersion = process.env.BUILD_VERSION ?? "dev";
const buildCommit = process.env.BUILD_COMMIT ?? "unknown";

const port = Number(process.env.FRONTEND_PORT ?? 9001);

/**
 * Bound to the loopback address explicitly.
 *
 * The default resolves "localhost", which can land on the IPv6 address alone.
 * A reviewer following how-to.md would then get a refused connection on
 * 127.0.0.1 with a server that says it is running. Set FRONTEND_HOST to
 * 0.0.0.0 to reach it from another machine or from a container.
 */
const host = process.env.FRONTEND_HOST ?? "127.0.0.1";

export default defineConfig({
    plugins: [sveltekit()],
    // Under vitest the browser build of Svelte is the one that mounts into
    // jsdom. Without this, the server build is resolved and no component ever
    // renders.
    resolve: process.env.VITEST ? { conditions: ["browser"] } : undefined,
    define: {
        __API_BASE_URL__: JSON.stringify(apiBaseUrl),
        __BUILD_VERSION__: JSON.stringify(buildVersion),
        __BUILD_COMMIT__: JSON.stringify(buildCommit),
    },
    server: {
        host,
        port,
        strictPort: true,
    },
    preview: {
        host,
        port,
        strictPort: true,
    },
    test: {
        environment: "jsdom",
        globals: true,
        include: ["src/**/*.test.ts"],
        setupFiles: ["src/tests/setup.ts"],
    },
});
