import { defineConfig } from "@playwright/test";

export default defineConfig({
	testDir: "e2e",
	use: {
		baseURL: "http://127.0.0.1:5173",
	},
	webServer: {
		// dev:web serves the renderer alone (VITE_NO_ELECTRON=1) — no Electron child to
		// launch, which is all the browser-based e2e suite needs. --host 127.0.0.1
		// forces an IPv4 bind: Vite's default binds ::1 only, and sandboxed macOS
		// doesn't route the IPv4 baseURL above to it.
		command: "npm run dev:web -- --port 5173 --host 127.0.0.1",
		port: 5173,
		reuseExistingServer: !process.env.CI,
		// Empty string = "trusted, same-origin" to lib/api-client's runtime base
		// URL: requests go through real fetch() with relative URLs, so specs can
		// stub daemon routes with page.route(). Unset (null) would short-circuit
		// every API call to a synthetic 503 before fetch, bypassing interception.
		env: { VITE_AO_API_BASE_URL: "" },
	},
});
