package org

// Overview is the read-model returned by GET /api/v1/org/overview: the whole
// holding tree in one call, so a CEO/PM orchestrator (or the desktop UI) can
// see current org state without walking company/project endpoints one by one.
type Overview struct {
	// HoldingHQ is the holding-wide CEO headquarters, or nil when none is
	// registered yet.
	HoldingHQ *HQInfo           `json:"holdingHq,omitempty"`
	Companies []CompanyOverview `json:"companies"`
	// Paused reflects the global heartbeat kill switch.
	Paused bool `json:"paused"`
}

// CompanyOverview is one company's slice of the org tree: its PM headquarters
// (if registered) and its ordinary delivery projects. The HQ project itself is
// never also listed in Projects.
type CompanyOverview struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	HQ       *HQInfo         `json:"hq,omitempty"`
	Projects []ProjectStatus `json:"projects"`
}

// HQInfo is the status of a company's PM or the holding's CEO headquarters
// project: its active orchestrator (if any) and its heartbeat configuration.
type HQInfo struct {
	ProjectID             string `json:"projectId"`
	OrchestratorSessionID string `json:"orchestratorSessionId,omitempty"`
	// Activity is the orchestrator's activity state (e.g. "idle", "active",
	// "waiting_input"), or "" when no active orchestrator is running.
	Activity          string `json:"activity,omitempty"`
	HeartbeatEnabled  bool   `json:"heartbeatEnabled"`
	HeartbeatInterval string `json:"heartbeatInterval,omitempty"`
}

// ProjectStatus is one ordinary delivery project's status within a company.
type ProjectStatus struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Kind                  string `json:"kind"`
	OrchestratorSessionID string `json:"orchestratorSessionId,omitempty"`
	OrchestratorActivity  string `json:"orchestratorActivity,omitempty"`
	ActiveSessions        int    `json:"activeSessions"`
	TotalSessions         int    `json:"totalSessions"`
}

// SetHQRoleInput is the body shape for PUT /api/v1/projects/{id}/hq. Role
// must be "company", "holding", or "" to clear the project's HQ role.
type SetHQRoleInput struct {
	Role string `json:"role"`
}

// SetHeartbeatPauseInput is the body shape for PUT /api/v1/org/heartbeat.
type SetHeartbeatPauseInput struct {
	Paused bool `json:"paused"`
}
