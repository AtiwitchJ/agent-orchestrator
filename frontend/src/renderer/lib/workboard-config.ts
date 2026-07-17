import type { components } from "../../api/schema";

export const WORKBOARD_ORCHESTRATOR_AGENT = "hermes";

export const DEFAULT_WORKBOARD_CONFIG: components["schemas"]["WorkboardConfig"] = {
	wipLimit: 3,
};

export function isWorkboardEnabled(config?: components["schemas"]["ProjectConfig"] | null): boolean {
	return config?.workboard !== undefined;
}
