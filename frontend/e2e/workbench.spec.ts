import { expect, test } from "@playwright/test";

// The Playwright web server runs `dev:web` (VITE_NO_ELECTRON=1), so
// useWorkspaceQuery serves the deterministic preview fixtures from
// lib/mock-data.ts instead of hitting a daemon. The tests run in Chromium
// (no window.ao), so the terminal shows its browser-preview surface.

test("renders the org sidebar shell with projects and their sessions", async ({ page }) => {
	await page.goto("/");
	// Org-tree sidebar: section label, both mock projects, and a worker row.
	await expect(page.getByText("Vertex Holdings Org")).toBeVisible();
	await expect(page.getByRole("button", { name: "api-gateway", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "webgl-preview", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Open auth stack" })).toBeVisible();
});

test("deep-links into a worker session", async ({ page }) => {
	// The renderer router uses hash history (src/renderer/router.tsx).
	await page.goto("/#/sessions/refactor-mux");
	// Session workbench = terminal pane plus the inspector rail.
	await expect(page.locator("#inspector")).toBeVisible();
});

test("drilling into a worker opens the session workbench", async ({ page }) => {
	await page.goto("/");
	await page.getByRole("button", { name: "Open Split terminal mux responsibilities" }).click();
	await expect(page).toHaveURL(/sessions\/refactor-mux/);
	await expect(page.locator("#inspector")).toBeVisible();
});
