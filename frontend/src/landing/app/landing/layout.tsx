import type { Metadata } from "next";

export const metadata: Metadata = {
	title: "Modern Agent",
	description:
		"Open-source platform for running parallel AI coding agents. Spawn Claude Code, Codex, Aider, and more in isolated worktrees — all managed from one dashboard.",
	openGraph: {
		type: "website",
		url: "https://modernagent.dev/landing",
		siteName: "Modern Agent",
		title: "Modern Agent",
		description:
			"Open-source platform for running parallel AI coding agents. Spawn Claude Code, Codex, Aider, and more in isolated worktrees — all managed from one dashboard.",
		images: [{ url: "/og-image.png", width: 1024, height: 1024, alt: "Modern Agent" }],
	},
	twitter: {
		card: "summary",
		site: "@modernagent",
		creator: "@modernagent",
		title: "Modern Agent",
		description:
			"Open-source platform for running parallel AI coding agents. Spawn Claude Code, Codex, Aider, and more in isolated worktrees — all managed from one dashboard.",
		images: ["/og-image.png"],
	},
	alternates: {
		canonical: "https://modernagent.dev/",
	},
};

export default function LandingLayout({ children }: { children: React.ReactNode }) {
	return <>{children}</>;
}
