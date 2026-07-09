// Package org implements the org-hierarchy use-cases for controllers: marking
// a project as a company's PM headquarters or the holding's CEO headquarters,
// reading the whole holding tree in one call, and the global heartbeat kill
// switch. Layout mirrors service/company: service.go (Manager + implementation),
// types.go (API-facing structs), store.go (the narrow Store interface this
// service needs).
package org

import (
	"context"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

// Manager is the controller-facing contract for the /api/v1/org surface plus
// the project-hq-role assignment endpoint.
type Manager interface {
	// SetHQRole sets (or, with an empty Role, clears) a project's HQ role.
	SetHQRole(ctx context.Context, projectID domain.ProjectID, in SetHQRoleInput) error

	// Overview returns the whole holding tree: the holding HQ, every company
	// with its HQ and delivery projects, and the current heartbeat pause state.
	Overview(ctx context.Context) (Overview, error)

	// HeartbeatPaused reports the current global heartbeat kill-switch state.
	HeartbeatPaused(ctx context.Context) (bool, error)
	// SetHeartbeatPaused sets the global heartbeat kill switch.
	SetHeartbeatPaused(ctx context.Context, paused bool) error

	// EnsureHoldingHQ returns the holding's auto-provisioned HQ project id,
	// creating it (no folder picker involved) on first call.
	EnsureHoldingHQ(ctx context.Context) (string, error)
	// EnsureCompanyHQ returns companyID's auto-provisioned PM HQ project id,
	// creating it (no folder picker involved) on first call.
	EnsureCompanyHQ(ctx context.Context, companyID string) (string, error)
}

// Service implements the org use-cases for controllers.
type Service struct {
	store Store
	// projects registers an auto-provisioned HQ repo as a project. nil in
	// deployments that construct Service via New (e.g. most tests): HQ
	// auto-provisioning is simply unavailable, everything else still works.
	projects ProjectCreator
	// dataDir is the daemon's data directory, the root under which
	// EnsureHoldingHQ/EnsureCompanyHQ provision HQ repos. Empty disables
	// auto-provisioning the same way a nil projects does.
	dataDir string
}

var _ Manager = (*Service)(nil)

// Deps captures optional collaborators for org use-cases.
type Deps struct {
	Store Store
	// Projects and DataDir together enable EnsureHoldingHQ/EnsureCompanyHQ.
	// Leave both zero to disable HQ auto-provisioning (e.g. in tests that
	// don't exercise it).
	Projects ProjectCreator
	DataDir  string
}

// New returns an org service backed by the given durable store. HQ
// auto-provisioning (EnsureHoldingHQ/EnsureCompanyHQ) is unavailable; use
// NewWithDeps to enable it.
func New(store Store) *Service {
	return NewWithDeps(Deps{Store: store})
}

// NewWithDeps returns an org service with optional collaborators.
func NewWithDeps(d Deps) *Service {
	return &Service{store: d.Store, projects: d.Projects, dataDir: d.DataDir}
}

// SetHQRole validates and persists a project's HQ role. A "company" role
// requires the project already be assigned to a company and requires that
// company have no other HQ; a "holding" role requires the project be
// unassigned and requires no other holding HQ exist. These checks are
// check-then-write (same class of race as SpawnOrchestrator's
// lockOrchestratorProject note) — the partial unique indexes added in
// migration 0027 are the backstop against a concurrent-write race.
func (m *Service) SetHQRole(ctx context.Context, projectID domain.ProjectID, in SetHQRoleInput) error {
	role := domain.HQRole(strings.TrimSpace(in.Role))
	switch role {
	case "", domain.HQRoleCompany, domain.HQRoleHolding:
	default:
		return apierr.Invalid("HQ_ROLE_INVALID", "Unknown hq role: must be \"company\", \"holding\", or empty to clear", nil)
	}

	project, ok, err := m.store.GetProject(ctx, string(projectID))
	if err != nil {
		return apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load project")
	}
	if !ok {
		return apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}

	switch role {
	case domain.HQRoleCompany:
		if project.CompanyID == "" {
			return apierr.Invalid("HQ_REQUIRES_COMPANY", "A company HQ project must be assigned to a company first", nil)
		}
		conflict, err := m.hasOtherHQ(ctx, projectID, func(p domain.ProjectRecord) bool {
			return p.HQRole == domain.HQRoleCompany && p.CompanyID == project.CompanyID
		})
		if err != nil {
			return err
		}
		if conflict {
			return apierr.Conflict("COMPANY_HQ_EXISTS", "This company already has an HQ project", nil)
		}
	case domain.HQRoleHolding:
		if project.CompanyID != "" {
			return apierr.Invalid("HOLDING_HQ_REQUIRES_NO_COMPANY", "The holding HQ project must not be assigned to a company", nil)
		}
		conflict, err := m.hasOtherHQ(ctx, projectID, func(p domain.ProjectRecord) bool {
			return p.HQRole == domain.HQRoleHolding
		})
		if err != nil {
			return err
		}
		if conflict {
			return apierr.Conflict("HOLDING_HQ_EXISTS", "A holding HQ project is already registered", nil)
		}
	}

	ok, err = m.store.SetProjectHQRole(ctx, string(projectID), role)
	if err != nil {
		return apierr.Internal("HQ_ROLE_SET_FAILED", "Failed to set project hq role")
	}
	if !ok {
		return apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	return nil
}

// hasOtherHQ reports whether any project other than projectID satisfies match.
func (m *Service) hasOtherHQ(ctx context.Context, projectID domain.ProjectID, match func(domain.ProjectRecord) bool) (bool, error) {
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return false, apierr.Internal("PROJECTS_LIST_FAILED", "Failed to load projects")
	}
	for _, p := range projects {
		if domain.ProjectID(p.ID) == projectID {
			continue
		}
		if match(p) {
			return true, nil
		}
	}
	return false, nil
}

// Overview returns the whole holding tree in one call.
func (m *Service) Overview(ctx context.Context) (Overview, error) {
	companies, err := m.store.ListCompanies(ctx)
	if err != nil {
		return Overview{}, apierr.Internal("ORG_OVERVIEW_FAILED", "Failed to load companies")
	}
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return Overview{}, apierr.Internal("ORG_OVERVIEW_FAILED", "Failed to load projects")
	}
	sessions, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return Overview{}, apierr.Internal("ORG_OVERVIEW_FAILED", "Failed to load sessions")
	}
	paused, err := m.HeartbeatPaused(ctx)
	if err != nil {
		return Overview{}, err
	}

	byProject := make(map[string][]domain.SessionRecord, len(sessions))
	for _, s := range sessions {
		byProject[string(s.ProjectID)] = append(byProject[string(s.ProjectID)], s)
	}

	byCompany := make(map[string][]domain.ProjectRecord)
	var holdingHQ *domain.ProjectRecord
	for i := range projects {
		p := projects[i]
		if p.HQRole == domain.HQRoleHolding {
			holdingHQ = &projects[i]
			continue
		}
		if p.CompanyID != "" {
			byCompany[p.CompanyID] = append(byCompany[p.CompanyID], p)
		}
	}

	out := Overview{Companies: []CompanyOverview{}, Paused: paused}
	if holdingHQ != nil {
		out.HoldingHQ = hqInfo(*holdingHQ, byProject[holdingHQ.ID])
	}
	for _, c := range companies {
		co := CompanyOverview{ID: c.ID, Name: c.Name, Projects: []ProjectStatus{}}
		for _, p := range byCompany[c.ID] {
			if p.HQRole == domain.HQRoleCompany {
				co.HQ = hqInfo(p, byProject[p.ID])
				continue
			}
			co.Projects = append(co.Projects, projectStatus(p, byProject[p.ID]))
		}
		out.Companies = append(out.Companies, co)
	}
	return out, nil
}

// HeartbeatPaused reports the current global heartbeat kill-switch state. An
// unset setting (never paused, or a fresh daemon) means not paused.
func (m *Service) HeartbeatPaused(ctx context.Context) (bool, error) {
	v, ok, err := m.store.GetOrgSetting(ctx, domain.OrgSettingHeartbeatPaused)
	if err != nil {
		return false, apierr.Internal("ORG_SETTING_LOAD_FAILED", "Failed to load heartbeat pause setting")
	}
	if !ok {
		return false, nil
	}
	return v == "true", nil
}

// SetHeartbeatPaused sets the global heartbeat kill switch.
func (m *Service) SetHeartbeatPaused(ctx context.Context, paused bool) error {
	value := "false"
	if paused {
		value = "true"
	}
	if err := m.store.SetOrgSetting(ctx, domain.OrgSettingHeartbeatPaused, value); err != nil {
		return apierr.Internal("ORG_SETTING_SET_FAILED", "Failed to set heartbeat pause setting")
	}
	return nil
}

func projectStatus(p domain.ProjectRecord, sessions []domain.SessionRecord) ProjectStatus {
	ps := ProjectStatus{ID: p.ID, Name: p.DisplayName, Kind: string(p.Kind.WithDefault())}
	for _, s := range sessions {
		if s.IsTerminated {
			continue
		}
		ps.TotalSessions++
		if s.Activity.State == domain.ActivityActive {
			ps.ActiveSessions++
		}
		if s.Kind == domain.KindOrchestrator {
			ps.OrchestratorSessionID = string(s.ID)
			ps.OrchestratorActivity = string(s.Activity.State)
		}
	}
	return ps
}

func hqInfo(p domain.ProjectRecord, sessions []domain.SessionRecord) *HQInfo {
	ps := projectStatus(p, sessions)
	return &HQInfo{
		ProjectID:             p.ID,
		OrchestratorSessionID: ps.OrchestratorSessionID,
		Activity:              ps.OrchestratorActivity,
		HeartbeatEnabled:      p.Config.Heartbeat.Enabled,
		HeartbeatInterval:     p.Config.Heartbeat.Interval,
	}
}
