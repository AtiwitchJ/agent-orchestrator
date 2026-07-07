// Runtime environment detection. The renderer is a single bundle that runs in
// the Electron desktop app AND in a plain browser against the same daemon
// (when served as a static SPA by the daemon's httpd SPA handler). Build-time
// flags (VITE_NO_ELECTRON) only steer the Vite config — the runtime check is
// the source of truth, since `window.ao` is injected by the Electron preload
// and is absent in the browser.
export const isElectron = (): boolean => typeof window !== "undefined" && Boolean(window.ao);

export const inWebMode = (): boolean => !isElectron();