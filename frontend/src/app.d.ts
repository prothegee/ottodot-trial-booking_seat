/**
 * Compile time constants injected by vite.config.ts.
 *
 * They are declared here so TypeScript knows they exist. A missing declaration
 * would push every reader towards reaching for process.env in a component,
 * which does not exist in a browser bundle.
 */
declare global {
    const __API_BASE_URL__: string;
    const __BUILD_VERSION__: string;
    const __BUILD_COMMIT__: string;

    namespace App {}
}

export {};
