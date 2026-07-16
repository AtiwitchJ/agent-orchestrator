import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useBrowserView } from "./useBrowserView";

describe("useBrowserView (web mode)", () => {
	const originalAo = (window as Window & { ao?: unknown }).ao;

	beforeEach(() => {
		delete (window as Window & { ao?: unknown }).ao;
	});

	afterEach(() => {
		if (originalAo === undefined) {
			delete (window as Window & { ao?: unknown }).ao;
		} else {
			(window as Window & { ao: unknown }).ao = originalAo;
		}
	});

	it("returns a stable no-op model when window.ao is absent", () => {
		const { result } = renderHook(() =>
			useBrowserView({
				sessionId: "sess-1",
				active: true,
				poppedOut: false,
				previewUrl: "http://127.0.0.1:5173",
				previewRevision: 1,
			}),
		);

		expect(result.current.viewId).toBe("");
		expect(result.current.navState.url).toBe("");
		expect(result.current.navState.canGoBack).toBe(false);

		// All navigation methods resolve without throwing.
		return act(async () => {
			await result.current.navigate("http://example.com");
			await result.current.goBack();
			await result.current.goForward();
			await result.current.reload();
			await result.current.stop();
			result.current.destroy();
		});
	});

	it("slotRef is callable and does nothing", () => {
		const { result } = renderHook(() =>
			useBrowserView({
				sessionId: "sess-2",
				active: false,
				poppedOut: false,
			}),
		);
		const node = document.createElement("div");
		expect(() => result.current.slotRef(node)).not.toThrow();
		expect(() => result.current.slotRef(null)).not.toThrow();
	});
});

describe("useBrowserView (electron mode)", () => {
	const originalAo = (window as Window & { ao?: unknown }).ao;

	beforeEach(() => {
		(window as unknown as { ao: unknown }).ao = {
			browser: {
				ensure: vi.fn(async (sessionId: string) => ({
					viewId: `view-${sessionId}`,
					url: "",
					title: "",
					canGoBack: false,
					canGoForward: false,
					isLoading: false,
				})),
				setBounds: vi.fn(),
				navigate: vi.fn(async () => ({
					viewId: "",
					url: "",
					title: "",
					canGoBack: false,
					canGoForward: false,
					isLoading: false,
				})),
				onNavState: vi.fn(() => () => undefined),
				destroy: vi.fn(),
			},
		};
	});

	afterEach(() => {
		if (originalAo === undefined) {
			delete (window as Window & { ao?: unknown }).ao;
		} else {
			(window as Window & { ao: unknown }).ao = originalAo;
		}
	});

	it("calls window.ao!.browser.ensure on mount", async () => {
		const ensure = (window as unknown as { ao: { browser: { ensure: ReturnType<typeof vi.fn> } } }).ao.browser.ensure;
		const { result } = renderHook(() =>
			useBrowserView({
				sessionId: "sess-electron",
				active: true,
				poppedOut: false,
			}),
		);
		await act(async () => {
			await Promise.resolve();
		});
		expect(ensure).toHaveBeenCalled();
		expect(result.current.viewId).toBe("view-sess-electron");
	});
});