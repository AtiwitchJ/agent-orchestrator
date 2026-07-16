// Shared cache key for the project-scoped Workboard query.
// The Workboard UI owns the query; the SSE transport only needs this key to
// invalidate it when a card changes.
export const workboardQueryKey = (projectId?: string) =>
	projectId ? (["workboard", projectId] as const) : (["workboard"] as const);
