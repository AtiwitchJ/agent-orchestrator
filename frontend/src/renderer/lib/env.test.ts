import { afterEach, describe, expect, it } from "vitest";
import { inWebMode, isElectron } from "./env";

describe("env", () => {
	const originalAo = (window as Window & { ao?: unknown }).ao;

	afterEach(() => {
		if (originalAo === undefined) {
			delete (window as Window & { ao?: unknown }).ao;
		} else {
			(window as Window & { ao: unknown }).ao = originalAo;
		}
	});

	it("isElectron is false when window.ao is undefined", () => {
		delete (window as Window & { ao?: unknown }).ao;
		expect(isElectron()).toBe(false);
		expect(inWebMode()).toBe(true);
	});

	it("isElectron is true when window.ao is present", () => {
		(window as unknown as { ao: unknown }).ao = { stub: true };
		expect(isElectron()).toBe(true);
		expect(inWebMode()).toBe(false);
	});

	it("isElectron tolerates a falsy window.ao value", () => {
		(window as unknown as { ao: unknown }).ao = null;
		expect(isElectron()).toBe(false);
	});
});