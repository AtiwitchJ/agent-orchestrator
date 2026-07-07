// build-web.mjs — produce the static SPA bundle the daemon serves at /.
//
// Equivalent to `VITE_NO_ELECTRON=1 vite build --config vite.renderer.config.ts
// --outDir dist-web`. Lives as a script so callers don't need to remember the
// flag and the outDir, and so package.json edits aren't required to add a
// script entry.
import { spawnSync } from "node:child_process";
import { dirname, resolve, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(scriptsDir, "..");
const configPath = join(frontendRoot, "vite.renderer.config.ts");
const outDir = join(frontendRoot, "dist-web");

const result = spawnSync(
	process.platform === "win32" ? "npx.cmd" : "npx",
	[
		"vite",
		"build",
		"--config",
		configPath,
		"--outDir",
		outDir,
	],
	{
		cwd: frontendRoot,
		stdio: "inherit",
		env: { ...process.env, VITE_NO_ELECTRON: "1" },
	},
);

if (result.error) {
	console.error(`failed to start vite build: ${result.error.message}`);
	process.exit(1);
}
process.exit(result.status ?? 1);