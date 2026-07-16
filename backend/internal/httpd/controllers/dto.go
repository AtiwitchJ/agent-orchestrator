package controllers

import (
	"encoding/json"
	"errors"
	"time"

	internalconfig "github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/legacyimport"
	"github.com/modernagent/modern-agent/backend/internal/policy"
	agentsvc "github.com/modernagent/modern-agent/backend/internal/service/agent"
	companysvc "github.com/modernagent/modern-agent/backend/internal/service/company"
	orgsvc "github.com/modernagent/modern-agent/backend/internal/service/org"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

// HTTP response envelopes for the projects surface — the SINGLE definition of
// each wire shape. The handlers encode these (envelope.WriteJSON), and
// apispec.Build reflects these same types into openapi.yaml, so the served
// contract and the generated spec can't disagree. The request side needs no
// wrappers: handlers decode the body straight into the project commands
// (projectsvc.AddInput), which apispec also reflects.

// ProjectIDParam is the {id} path parameter shared by the /projects/{id}
// routes. Handlers read it via chi.URLParam (see projectID); it is declared here
// so every wire input/output shape has one home, and apispec.Build reflects it
// as the path parameter.
type ProjectIDParam struct {
	ID string `path:"id" description:"Project identifier (registry key)."`
}

// WorkboardProjectIDParam is the project path parameter for workboard routes.
type WorkboardProjectIDParam struct {
	ProjectID string `path:"projectId" description:"Project identifier."`
}

// WorkCardIDParam is the work-card path parameter shared by card routes.
type WorkCardIDParam struct {
	CardID string `path:"cardId" description:"Work card identifier."`
}

// WorkCardResponse is the durable work-card read model exposed by the API.
// Waiting and retargeting flags remain independent durable facts; clients
// derive any display badge from them at read time.
type WorkCardResponse struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"projectId"`
	BoardID            string     `json:"boardId"`
	Title              string     `json:"title"`
	Notes              string     `json:"notes"`
	Priority           string     `json:"priority" enum:"low,normal,high,urgent"`
	Labels             []string   `json:"labels"`
	Status             string     `json:"status" enum:"triage,backlog,todo,scheduled,ready,running,review,blocked,done"`
	ScheduledAt        *time.Time `json:"scheduledAt,omitempty"`
	ReadyAt            *time.Time `json:"readyAt,omitempty"`
	Position           int64      `json:"position"`
	TargetPath         string     `json:"targetPath"`
	RepoName           string     `json:"repoName,omitempty"`
	Agent              string     `json:"agent"`
	SessionID          string     `json:"sessionId,omitempty"`
	WaitingForInput    bool       `json:"waitingForInput"`
	PausedRetarget     bool       `json:"pausedRetarget"`
	GoalVersion        int        `json:"goalVersion"`
	SupersededByCardID string     `json:"supersededByCardId,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// CreateWorkCardRequest is the body of POST /api/v1/projects/{projectId}/workboard/cards.
type CreateWorkCardRequest struct {
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	Priority    string     `json:"priority" enum:"low,normal,high,urgent"`
	Labels      []string   `json:"labels"`
	Status      string     `json:"status,omitempty" enum:"triage,backlog,todo,scheduled,ready,running,review,blocked,done"`
	TargetPath  string     `json:"targetPath"`
	Agent       string     `json:"agent"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
}

func (r CreateWorkCardRequest) toInput(projectID string) workboardsvc.CreateInput {
	return workboardsvc.CreateInput{
		ProjectID: projectID, Title: r.Title, Notes: r.Notes,
		Priority: domain.CardPriority(r.Priority), Labels: r.Labels, Status: domain.CardStatus(r.Status),
		TargetPath: r.TargetPath, Agent: r.Agent, ScheduledAt: r.ScheduledAt,
	}
}

// UpdateWorkCardRequest is the body of PATCH /api/v1/workboard/cards/{cardId}.
type UpdateWorkCardRequest struct {
	Title       *string    `json:"title,omitempty"`
	Notes       *string    `json:"notes,omitempty"`
	Priority    *string    `json:"priority,omitempty" enum:"low,normal,high,urgent"`
	Labels      *[]string  `json:"labels,omitempty"`
	Status      *string    `json:"status,omitempty" enum:"triage,backlog,todo,scheduled,ready,running,review,blocked,done"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	TargetPath  *string    `json:"targetPath,omitempty"`
	Agent       *string    `json:"agent,omitempty"`
	Position    *int64     `json:"position,omitempty"`
}

func (r UpdateWorkCardRequest) toInput() workboardsvc.UpdateInput {
	in := workboardsvc.UpdateInput{
		Title: r.Title, Notes: r.Notes, Labels: r.Labels, ScheduledAt: r.ScheduledAt,
		TargetPath: r.TargetPath, Agent: r.Agent, Position: r.Position,
	}
	if r.Priority != nil {
		priority := domain.CardPriority(*r.Priority)
		in.Priority = &priority
	}
	if r.Status != nil {
		status := domain.CardStatus(*r.Status)
		in.Status = &status
	}
	return in
}

// MoveWorkCardRequest is the body of POST /api/v1/workboard/cards/{cardId}/move.
type MoveWorkCardRequest struct {
	Status   string `json:"status" enum:"triage,backlog,todo,scheduled,ready,running,review,blocked,done"`
	Position int64  `json:"position"`
}

// ListWorkCardsResponse is the body of GET /api/v1/projects/{projectId}/workboard/cards.
type ListWorkCardsResponse struct {
	Cards []WorkCardResponse `json:"cards"`
}

func newWorkCardResponse(card domain.WorkCard) WorkCardResponse {
	return WorkCardResponse{
		ID: card.ID, ProjectID: card.ProjectID, BoardID: card.BoardID, Title: card.Title, Notes: card.Notes,
		Priority: string(card.Priority), Labels: append([]string(nil), card.Labels...), Status: string(card.Status),
		ScheduledAt: card.ScheduledAt, ReadyAt: card.ReadyAt, Position: card.Position, TargetPath: card.TargetPath,
		RepoName: card.RepoName, Agent: card.Agent, SessionID: card.SessionID, WaitingForInput: card.WaitingForInput,
		PausedRetarget: card.PausedRetarget, GoalVersion: card.GoalVersion, SupersededByCardID: card.SupersededByCardID,
		CreatedAt: card.CreatedAt, UpdatedAt: card.UpdatedAt,
	}
}

func workCardResponses(cards []domain.WorkCard) []WorkCardResponse {
	if cards == nil {
		return []WorkCardResponse{}
	}
	responses := make([]WorkCardResponse, len(cards))
	for i, card := range cards {
		responses[i] = newWorkCardResponse(card)
	}
	return responses
}

// ListProjectsResponse is the body of GET /api/v1/projects.
type ListProjectsResponse struct {
	Projects []projectsvc.Summary `json:"projects"`
}

// ProjectResponse is the { project } body shared by POST /projects (201).
type ProjectResponse struct {
	Project projectsvc.Project `json:"project"`
}

// GetProjectResponse is the { status, project } body of GET /projects/{id},
// where project is oneOf Project|Degraded discriminated by status.
type GetProjectResponse struct {
	Status  string            `json:"status" enum:"ok,degraded"`
	Project ProjectOrDegraded `json:"project"`
}

// ProjectOrDegraded is the discriminated `project` field: exactly one of
// Project/Degraded is set. It marshals as whichever is present (so the handler
// emits the right object) and exposes the oneOf variants to the spec reflector
// (so apispec.Build emits `oneOf: [Project, Degraded]`) — one type, both jobs.
type ProjectOrDegraded struct {
	Project  *projectsvc.Project
	Degraded *projectsvc.Degraded
}

// MarshalJSON encodes whichever variant is set (Project or Degraded).
func (p ProjectOrDegraded) MarshalJSON() ([]byte, error) {
	switch {
	case p.Degraded != nil:
		return json.Marshal(p.Degraded)
	case p.Project != nil:
		return json.Marshal(p.Project)
	default:
		// Unreachable in practice: the handler validates the GetResult via
		// newGetProjectResponse and writes a 500 before committing the 200
		// status, so this never encodes. Kept as a last-resort backstop —
		// erroring is still better than emitting a contract-breaking `null`,
		// though by here the status is already sent, so the real guard is
		// upstream.
		return nil, errEmptyProjectOrDegraded
	}
}

// errEmptyProjectOrDegraded marks a GetResult that set neither variant — a
// Manager-contract violation. newGetProjectResponse returns it so the handler
// can map it to a 500 before any response bytes are written.
var errEmptyProjectOrDegraded = errors.New("controllers: GetResult has neither Project nor Degraded set")

// JSONSchemaOneOf is read by swaggest's reflector (apispec.Build) to emit the
// oneOf for this field; it is not used at runtime.
func (ProjectOrDegraded) JSONSchemaOneOf() []interface{} {
	return []interface{}{projectsvc.Project{}, projectsvc.Degraded{}}
}

// newGetProjectResponse maps the internal GetResult onto the wire envelope —
// the explicit project→httpd boundary the result type exists for. It errors
// when the result sets neither variant, so the handler can return a clean 500
// BEFORE writing the 200 status rather than flushing a truncated body.
func newGetProjectResponse(res projectsvc.GetResult) (GetProjectResponse, error) {
	if res.Project == nil && res.Degraded == nil {
		return GetProjectResponse{}, errEmptyProjectOrDegraded
	}
	return GetProjectResponse{
		Status:  res.Status,
		Project: ProjectOrDegraded{Project: res.Project, Degraded: res.Degraded},
	}, nil
}

// SessionIDParam is the {sessionId} path parameter shared by session routes.
type SessionIDParam struct {
	SessionID string `path:"sessionId" description:"Session identifier, e.g. project-1."`
}

// ListSessionsQuery is the query string accepted by GET /api/v1/sessions.
type ListSessionsQuery struct {
	Project          string `query:"project,omitempty" description:"Project id filter."`
	Active           *bool  `query:"active,omitempty" description:"When true, return non-terminated sessions; when false, return terminated sessions."`
	OrchestratorOnly *bool  `query:"orchestratorOnly,omitempty" description:"When true, return only orchestrator sessions."`
	Fresh            *bool  `query:"fresh,omitempty" description:"When true, return only fresh non-terminated sessions."`
}

// CleanupSessionsQuery is the query string accepted by POST /api/v1/sessions/cleanup.
type CleanupSessionsQuery struct {
	Project string `query:"project,omitempty" description:"Project id filter. When omitted, clean terminated sessions across all projects."`
}

// SessionView is the session wire shape: the domain read model plus the
// display-safe branch name and the session's attributed pull requests in the
// curated SessionPRFacts shape. One session can own many PRs (e.g. a stack), so
// prs is a list. The embedded domain.Session.Metadata and domain.Session.PRs
// fields are json:"-"; these curated fields are what serialize.
type SessionView struct {
	domain.Session
	Branch string `json:"branch,omitempty"`
	// PreviewURL is the browser preview target the desktop app opens for this
	// session, set via POST /sessions/{sessionId}/preview. Empty (omitted) when
	// no preview has been requested. Pulled from the json:"-" domain Metadata.
	PreviewURL string `json:"previewUrl,omitempty"`
	// PreviewRevision bumps on every `ao preview` call (even when previewUrl is
	// unchanged) so the desktop browser panel can re-navigate / refresh on a
	// repeated preview of the same target. Pulled from the json:"-" domain
	// Metadata.
	PreviewRevision int64            `json:"previewRevision,omitempty"`
	PRs             []SessionPRFacts `json:"prs"`
}

// ListSessionsResponse is the body of GET /api/v1/sessions.
type ListSessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

// SpawnSessionRequest is the body of POST /api/v1/sessions.
type SpawnSessionRequest struct {
	ProjectID domain.ProjectID    `json:"projectId"`
	IssueID   domain.IssueID      `json:"issueId,omitempty"`
	Kind      domain.SessionKind  `json:"kind,omitempty" enum:"worker,orchestrator"`
	Harness   domain.AgentHarness `json:"harness,omitempty" enum:"claude-code,codex,aider,opencode,grok,droid,amp,agy,crush,cursor,qwen,copilot,goose,auggie,continue,devin,cline,kimi,kiro,kilocode,vibe,pi,autohand,command"`
	Branch    string              `json:"branch,omitempty"`
	Prompt    string              `json:"prompt,omitempty" maxLength:"4096"`
	// DisplayName is the sidebar label for the session, capped at 20 characters.
	// `ao spawn --name` always sets it; other clients (e.g. the desktop new-task
	// dialog) may omit it and fall back to the session id in the read model.
	DisplayName string `json:"displayName,omitempty" maxLength:"20"`
}

// SessionResponse is the { session } body shared by session create/get.
type SessionResponse struct {
	Session SessionView `json:"session"`
}

// SessionPreviewResponse is the body of GET /api/v1/sessions/{sessionId}/preview.
type SessionPreviewResponse struct {
	SessionID  domain.SessionID `json:"sessionId"`
	PreviewURL string           `json:"previewUrl,omitempty"`
	Entry      string           `json:"entry,omitempty"`
}

// RenameSessionRequest is the body of PATCH /api/v1/sessions/{sessionId}.
type RenameSessionRequest struct {
	DisplayName string `json:"displayName" minLength:"1"`
}

// SetSessionPreviewRequest is the body of POST /api/v1/sessions/{sessionId}/preview.
// An empty url asks the daemon to autodetect a static entry point in the
// session workspace; a non-empty url is used verbatim as the preview target.
type SetSessionPreviewRequest struct {
	URL string `json:"url,omitempty" description:"Preview target URL. When empty, the daemon autodetects a static entry point in the session workspace."`
}

// RenameSessionResponse is the body of PATCH /api/v1/sessions/{sessionId}.
type RenameSessionResponse struct {
	OK          bool             `json:"ok"`
	SessionID   domain.SessionID `json:"sessionId"`
	DisplayName string           `json:"displayName"`
}

// RestoreSessionResponse is the body of POST /api/v1/sessions/{sessionId}/restore.
type RestoreSessionResponse struct {
	OK        bool             `json:"ok"`
	SessionID domain.SessionID `json:"sessionId"`
	Session   SessionView      `json:"session"`
}

// KillSessionResponse is the body of POST /api/v1/sessions/{sessionId}/kill.
type KillSessionResponse struct {
	OK        bool             `json:"ok"`
	SessionID domain.SessionID `json:"sessionId"`
	Freed     bool             `json:"freed,omitempty"`
}

// RollbackSessionResponse is the body of POST /api/v1/sessions/{sessionId}/rollback.
// Exactly one of Deleted/Killed is true on a successful rollback; both are
// false when the session was already absent or already terminated (benign).
type RollbackSessionResponse struct {
	OK        bool             `json:"ok"`
	SessionID domain.SessionID `json:"sessionId"`
	Deleted   bool             `json:"deleted,omitempty"`
	Killed    bool             `json:"killed,omitempty"`
}

// CleanupSkippedSession is one terminal session whose workspace cleanup
// preserved rather than reclaimed (a dirty worktree is never force-deleted),
// with the user-facing reason.
type CleanupSkippedSession struct {
	SessionID domain.SessionID `json:"sessionId"`
	Reason    string           `json:"reason"`
}

// CleanupSessionsResponse is the body of POST /api/v1/sessions/cleanup.
type CleanupSessionsResponse struct {
	OK      bool                    `json:"ok"`
	Cleaned []domain.SessionID      `json:"cleaned"`
	Skipped []CleanupSkippedSession `json:"skipped"`
}

// SendSessionMessageRequest is the body of POST /api/v1/sessions/{sessionId}/send.
type SendSessionMessageRequest struct {
	Message string `json:"message" minLength:"1" maxLength:"4096"`
	// SenderSessionID names the sending agent session for an agent-to-agent
	// send (e.g. `ao send` run with AO_SESSION_ID set). Omitted/empty means a
	// human sent the message.
	//
	// Trust boundary: this is caller-supplied and only checked to name an
	// existing session, never verified against the identity of the HTTP
	// caller — there is no session-level auth on this daemon's localhost
	// trust model, so any process that can reach this endpoint can claim to
	// be any session. Do not treat the persisted sender as proof of who sent
	// the message.
	SenderSessionID string `json:"senderSessionId,omitempty"`
}

// SendSessionMessageResponse is the body of POST /api/v1/sessions/{sessionId}/send.
type SendSessionMessageResponse struct {
	OK        bool             `json:"ok"`
	SessionID domain.SessionID `json:"sessionId"`
	Message   string           `json:"message"`
}

// SessionPRFacts is the pull-request read shape returned under session PR routes.
type SessionPRFacts struct {
	URL            string                `json:"url"`
	Number         int                   `json:"number"`
	State          string                `json:"state" enum:"draft,open,merged,closed"`
	CI             domain.CIState        `json:"ci" enum:"unknown,pending,passing,failing"`
	Review         domain.ReviewDecision `json:"review" enum:"none,approved,changes_requested,review_required"`
	Mergeability   domain.Mergeability   `json:"mergeability" enum:"unknown,mergeable,conflicting,blocked,unstable"`
	ReviewComments bool                  `json:"reviewComments"`
	UpdatedAt      time.Time             `json:"updatedAt"`
}

// SessionPRSummary is the concise desktop SCM read model returned by GET
// /sessions/{sessionId}/pr. It intentionally omits CI log tails and review
// comment bodies.
type SessionPRSummary struct {
	URL              string                       `json:"url"`
	HTMLURL          string                       `json:"htmlUrl,omitempty"`
	Number           int                          `json:"number"`
	Title            string                       `json:"title"`
	State            domain.PRState               `json:"state" enum:"draft,open,merged,closed"`
	Provider         string                       `json:"provider" enum:"github"`
	Repo             string                       `json:"repo"`
	Author           string                       `json:"author"`
	SourceBranch     string                       `json:"sourceBranch"`
	TargetBranch     string                       `json:"targetBranch"`
	HeadSHA          string                       `json:"headSha"`
	Additions        int                          `json:"additions"`
	Deletions        int                          `json:"deletions"`
	ChangedFiles     int                          `json:"changedFiles"`
	CI               SessionPRCISummary           `json:"ci"`
	Review           SessionPRReviewSummary       `json:"review"`
	Mergeability     SessionPRMergeabilitySummary `json:"mergeability"`
	UpdatedAt        time.Time                    `json:"updatedAt"`
	ObservedAt       time.Time                    `json:"observedAt,omitempty"`
	CIObservedAt     time.Time                    `json:"ciObservedAt,omitempty"`
	ReviewObservedAt time.Time                    `json:"reviewObservedAt,omitempty"`
}

// SessionPRCISummary is the CI status block for a session PR summary.
type SessionPRCISummary struct {
	State         domain.CIState          `json:"state" enum:"unknown,pending,passing,failing"`
	FailingChecks []SessionPRFailingCheck `json:"failingChecks"`
}

// SessionPRFailingCheck is one failed or cancelled CI check for a PR.
type SessionPRFailingCheck struct {
	Name       string               `json:"name"`
	Status     domain.PRCheckStatus `json:"status" enum:"failed,cancelled"`
	Conclusion string               `json:"conclusion"`
	URL        string               `json:"url,omitempty"`
}

// SessionPRReviewSummary is the review state block for a session PR summary.
type SessionPRReviewSummary struct {
	Decision                   domain.ReviewDecision         `json:"decision" enum:"none,approved,changes_requested,review_required"`
	HasUnresolvedHumanComments bool                          `json:"hasUnresolvedHumanComments"`
	UnresolvedBy               []SessionPRUnresolvedReviewer `json:"unresolvedBy"`
}

// SessionPRUnresolvedReviewer groups unresolved human comments by reviewer.
type SessionPRUnresolvedReviewer struct {
	ReviewerID string                       `json:"reviewerId"`
	Count      int                          `json:"count"`
	Links      []SessionPRReviewCommentLink `json:"links"`
	ReviewURL  string                       `json:"reviewUrl,omitempty"`
	IsBot      bool                         `json:"isBot,omitempty"`
}

// SessionPRReviewCommentLink points to one unresolved review comment.
type SessionPRReviewCommentLink struct {
	URL  string `json:"url,omitempty"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// SessionPRMergeabilitySummary is the mergeability block for a session PR summary.
type SessionPRMergeabilitySummary struct {
	State         domain.Mergeability     `json:"state" enum:"unknown,mergeable,conflicting,blocked,unstable"`
	Reasons       []string                `json:"reasons"`
	PRURL         string                  `json:"prUrl"`
	ConflictFiles []SessionPRConflictFile `json:"conflictFiles,omitempty"`
}

// SessionPRConflictFile is one file involved in a PR merge conflict.
type SessionPRConflictFile struct {
	Path string `json:"path"`
	URL  string `json:"url,omitempty"`
}

// ListSessionPRsResponse is the body of GET /sessions/{sessionId}/pr.
type ListSessionPRsResponse struct {
	SessionID domain.SessionID   `json:"sessionId"`
	PRs       []SessionPRSummary `json:"prs"`
}

// NewSessionPRSummary maps the service PR summary model to its HTTP DTO.
func NewSessionPRSummary(in sessionsvc.PRSummary) SessionPRSummary {
	return SessionPRSummary{
		URL:              in.URL,
		HTMLURL:          in.HTMLURL,
		Number:           in.Number,
		Title:            in.Title,
		State:            in.State,
		Provider:         in.Provider,
		Repo:             in.Repo,
		Author:           in.Author,
		SourceBranch:     in.SourceBranch,
		TargetBranch:     in.TargetBranch,
		HeadSHA:          in.HeadSHA,
		Additions:        in.Additions,
		Deletions:        in.Deletions,
		ChangedFiles:     in.ChangedFiles,
		CI:               newSessionPRCISummary(in.CI),
		Review:           newSessionPRReviewSummary(in.Review),
		Mergeability:     newSessionPRMergeabilitySummary(in.Mergeability),
		UpdatedAt:        in.UpdatedAt,
		ObservedAt:       in.ObservedAt,
		CIObservedAt:     in.CIObservedAt,
		ReviewObservedAt: in.ReviewObservedAt,
	}
}

func newSessionPRCISummary(in sessionsvc.PRCISummary) SessionPRCISummary {
	checks := make([]SessionPRFailingCheck, 0, len(in.FailingChecks))
	for _, ch := range in.FailingChecks {
		checks = append(checks, SessionPRFailingCheck{Name: ch.Name, Status: ch.Status, Conclusion: ch.Conclusion, URL: ch.URL})
	}
	return SessionPRCISummary{State: in.State, FailingChecks: checks}
}

func newSessionPRReviewSummary(in sessionsvc.PRReviewSummary) SessionPRReviewSummary {
	reviewers := make([]SessionPRUnresolvedReviewer, 0, len(in.UnresolvedBy))
	for _, reviewer := range in.UnresolvedBy {
		links := make([]SessionPRReviewCommentLink, 0, len(reviewer.Links))
		for _, link := range reviewer.Links {
			links = append(links, SessionPRReviewCommentLink{URL: link.URL, File: link.File, Line: link.Line})
		}
		reviewers = append(reviewers, SessionPRUnresolvedReviewer{ReviewerID: reviewer.ReviewerID, Count: reviewer.Count, Links: links, ReviewURL: reviewer.ReviewURL, IsBot: reviewer.IsBot})
	}
	return SessionPRReviewSummary{Decision: in.Decision, HasUnresolvedHumanComments: in.HasUnresolvedHumanComments, UnresolvedBy: reviewers}
}

func newSessionPRMergeabilitySummary(in sessionsvc.PRMergeabilitySummary) SessionPRMergeabilitySummary {
	files := make([]SessionPRConflictFile, 0, len(in.ConflictFiles))
	for _, file := range in.ConflictFiles {
		files = append(files, SessionPRConflictFile{Path: file.Path, URL: file.URL})
	}
	return SessionPRMergeabilitySummary{State: in.State, Reasons: in.Reasons, PRURL: in.PRURL, ConflictFiles: files}
}

// ClaimPRRequest is the body of POST /sessions/{sessionId}/pr/claim.
type ClaimPRRequest struct {
	PR            string `json:"pr" minLength:"1"`
	AllowTakeover *bool  `json:"allowTakeover,omitempty"`
}

// ClaimPRResponse is the body of POST /sessions/{sessionId}/pr/claim.
type ClaimPRResponse struct {
	OK            bool               `json:"ok"`
	SessionID     domain.SessionID   `json:"sessionId"`
	PRs           []SessionPRFacts   `json:"prs"`
	BranchChanged bool               `json:"branchChanged"`
	TakenOverFrom []domain.SessionID `json:"takenOverFrom"`
}

// SetActivityRequest is the body of POST /api/v1/sessions/{sessionId}/activity.
type SetActivityRequest struct {
	State string `json:"state" enum:"active,idle,waiting_input,exited" description:"Agent activity state reported by an agent hook."`
}

// SetActivityResponse is the body of POST /api/v1/sessions/{sessionId}/activity.
type SetActivityResponse struct {
	OK        bool             `json:"ok"`
	SessionID domain.SessionID `json:"sessionId"`
	State     string           `json:"state"`
}

// OrchestratorIDParam is the {id} path parameter for orchestrator routes.
type OrchestratorIDParam struct {
	ID string `path:"id" description:"Orchestrator session identifier, e.g. project-orchestrator."`
}

// SpawnOrchestratorRequest is the body of POST /api/v1/orchestrators.
type SpawnOrchestratorRequest struct {
	ProjectID domain.ProjectID `json:"projectId"`
	Clean     bool             `json:"clean,omitempty"`
}

// SpawnOrchestratorResponse is the body of POST /api/v1/orchestrators.
type SpawnOrchestratorResponse struct {
	Orchestrator OrchestratorResponse `json:"orchestrator"`
}

// OrchestratorResponse is the minimal orchestrator read model returned after spawn.
type OrchestratorResponse struct {
	ID          domain.SessionID `json:"id"`
	ProjectID   domain.ProjectID `json:"projectId"`
	ProjectName string           `json:"projectName,omitempty"`
}

// ListAgentsResponse is the body of GET /api/v1/agents.
type ListAgentsResponse = agentsvc.Inventory

// RefreshAgentsResponse is the body of POST /api/v1/agents/refresh.
type RefreshAgentsResponse = agentsvc.Inventory

// AgentInfo is one supported or installed agent entry.
type AgentInfo = agentsvc.Info

// ListNotificationsQuery is the query string accepted by GET /api/v1/notifications.
type ListNotificationsQuery struct {
	Status string `query:"status,omitempty" enum:"unread" description:"Notification status filter. V1 supports only unread."`
	Limit  int    `query:"limit,omitempty" minimum:"1" maximum:"100" description:"Maximum notifications to return. Defaults to 50; capped at 100."`
}

// NotificationStreamQuery is the query string accepted by GET /api/v1/notifications/stream.
type NotificationStreamQuery struct {
	ProjectID string `query:"projectId,omitempty" description:"Optional project id filter for live notifications."`
}

// NotificationIDParam is the {id} path parameter shared by notification routes.
type NotificationIDParam struct {
	ID string `path:"id" description:"Notification identifier."`
}

// NotificationTarget is the dashboard navigation target for a notification.
type NotificationTarget struct {
	Kind      string `json:"kind" enum:"session,pr"`
	SessionID string `json:"sessionId"`
	PRURL     string `json:"prUrl,omitempty"`
}

// NotificationResponse is one stored notification returned by the API.
type NotificationResponse struct {
	ID        string             `json:"id"`
	SessionID string             `json:"sessionId"`
	ProjectID string             `json:"projectId"`
	PRURL     string             `json:"prUrl"`
	Type      string             `json:"type" enum:"needs_input,ready_to_merge,pr_merged,pr_closed_unmerged,auto_terminated"`
	Title     string             `json:"title"`
	Body      string             `json:"body"`
	Status    string             `json:"status" enum:"unread,read"`
	CreatedAt time.Time          `json:"createdAt"`
	Target    NotificationTarget `json:"target"`
}

// ListNotificationsResponse is the body of GET /api/v1/notifications.
type ListNotificationsResponse struct {
	Notifications []NotificationResponse `json:"notifications"`
}

// MarkNotificationReadRequest is the body of PATCH /api/v1/notifications/{id}.
type MarkNotificationReadRequest struct {
	Status string `json:"status" enum:"read" description:"V1 supports only marking an unread notification read."`
}

// NotificationEnvelope is the { notification } response body for notification mutations.
type NotificationEnvelope struct {
	Notification NotificationResponse `json:"notification"`
}

// MarkAllNotificationsReadResponse is the body of POST /api/v1/notifications/read-all.
type MarkAllNotificationsReadResponse struct {
	Notifications []NotificationResponse `json:"notifications"`
}

// ImportStatusResponse is the body of GET /api/v1/import: whether a legacy AO
// install is available to import, and the root the daemon would read from.
type ImportStatusResponse struct {
	Available  bool   `json:"available"`
	LegacyRoot string `json:"legacyRoot"`
}

// ImportRunResponse is the body of POST /api/v1/import: the structured outcome
// of the import run (counts + notes), reused verbatim from the import engine.
type ImportRunResponse struct {
	Report legacyimport.Report `json:"report"`
}

// PRIDParam is the {id} path parameter shared by the /prs/{id} routes.
type PRIDParam struct {
	ID string `path:"id" description:"PR number."`
}

// MergePRResponse is the body of POST /api/v1/prs/{id}/merge (200).
type MergePRResponse struct {
	OK       bool   `json:"ok"`
	PRNumber int    `json:"prNumber"`
	Method   string `json:"method"`
}

// ResolveCommentsRequest is the optional body of POST /api/v1/prs/{id}/resolve-comments.
type ResolveCommentsRequest struct {
	CommentIDs []string `json:"commentIds,omitempty"`
}

// ResolveCommentsResponse is the body of POST /api/v1/prs/{id}/resolve-comments (200).
type ResolveCommentsResponse struct {
	OK       bool `json:"ok"`
	Resolved int  `json:"resolved"`
}

// ListCompaniesResponse is the body of GET /api/v1/companies.
type ListCompaniesResponse struct {
	Companies []companysvc.Company `json:"companies"`
}

// CompanyResponse is the { company } body of POST /api/v1/companies (201).
type CompanyResponse struct {
	Company companysvc.Company `json:"company"`
}

// AssignProjectCompanyResponse is the body of PUT /api/v1/projects/{id}/company
// (200). CompanyID echoes the now-current assignment ("" means unassigned).
type AssignProjectCompanyResponse struct {
	ProjectID string `json:"projectId"`
	CompanyID string `json:"companyId,omitempty"`
}

// CompanyIDParam is the {id} path parameter for company routes.
type CompanyIDParam struct {
	ID string `path:"id" description:"Company identifier."`
}

// DeleteCompanyResponse is the body of DELETE /api/v1/companies/{id}.
type DeleteCompanyResponse struct {
	Deleted bool `json:"deleted"`
}

// OrgOverviewResponse is the body of GET /api/v1/org/overview.
type OrgOverviewResponse struct {
	Overview orgsvc.Overview `json:"overview"`
}

// OrgHeartbeatResponse is the body of GET/PUT /api/v1/org/heartbeat.
type OrgHeartbeatResponse struct {
	Paused bool `json:"paused"`
}

// SetProjectHQRoleResponse is the body of PUT /api/v1/projects/{id}/hq (200).
// Role echoes the now-current hq role ("" means cleared).
type SetProjectHQRoleResponse struct {
	ProjectID string `json:"projectId"`
	Role      string `json:"role,omitempty"`
}

// EnsureHQResponse is the body of POST /api/v1/org/holding-hq and
// POST /api/v1/org/companies/{companyId}/hq (200). ProjectID is the
// auto-provisioned (or already-existing) HQ project's id.
type EnsureHQResponse struct {
	ProjectID string `json:"projectId"`
}

// OrgCompanyIDParam is the {companyId} path parameter for
// POST /api/v1/org/companies/{companyId}/hq.
type OrgCompanyIDParam struct {
	CompanyID string `path:"companyId" description:"Company identifier."`
}

// ListProjectMessagesQuery is the query string accepted by
// GET /api/v1/projects/{id}/messages.
type ListProjectMessagesQuery struct {
	Limit int `query:"limit,omitempty" minimum:"1" maximum:"500" description:"Maximum messages to return. Defaults to 100; capped at 500."`
}

// SessionMessage is the wire shape of one durable agent-to-agent (or
// human-to-agent) `ao send` message, mirroring domain.SessionMessageRecord
// with no derived fields. SenderSessionID is omitted when a human sent it.
type SessionMessage struct {
	ID              string    `json:"id"`
	SenderSessionID string    `json:"senderSessionId,omitempty"`
	TargetSessionID string    `json:"targetSessionId"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"createdAt"`
}

// ListProjectMessagesResponse is the body of GET /api/v1/projects/{id}/messages.
type ListProjectMessagesResponse struct {
	Messages []SessionMessage `json:"messages"`
}

// PolicyConfigDTO is the wire shape of the policy engine configuration for
// GET/PUT /api/v1/projects/{id}/policy. Field names are camelCase to match the
// rest of the JSON API surface — distinct from the snake_case tags on the
// persisted internalconfig.PolicyConfig blob (domain.ProjectConfig.Policy).
// The CLI's internal/cli/policy.go policyConfigDTO mirrors this shape exactly.
type PolicyConfigDTO struct {
	Enabled              bool   `json:"enabled,omitempty"`
	TrackerLabel         string `json:"trackerLabel,omitempty"`
	AutoFixOnCIFailure   bool   `json:"autoFixOnCiFailure,omitempty"`
	MaxAutoFixRounds     int    `json:"maxAutoFixRounds,omitempty"`
	RequireAgentReview   bool   `json:"requireAgentReview,omitempty"`
	ReviewStrategy       string `json:"reviewStrategy,omitempty"`
	ReviewAgent          string `json:"reviewAgent,omitempty"`
	MaxReviseRounds      int    `json:"maxReviseRounds,omitempty"`
	RequireHumanApproval bool   `json:"requireHumanApproval,omitempty"`
	HumanTimeoutHours    int    `json:"humanTimeoutHours,omitempty"`
	AgentFinalPass       bool   `json:"agentFinalPass,omitempty"`
	VetoSecondAgent      string `json:"vetoSecondAgent,omitempty"`
	MergeStrategy        string `json:"mergeStrategy,omitempty"`
	MinPRAgeMinutes      int    `json:"minPrAgeMinutes,omitempty"`
	BlockOnDraft         bool   `json:"blockOnDraft,omitempty"`
}

// newPolicyConfigDTO maps the persisted policy config onto its wire shape.
func newPolicyConfigDTO(c internalconfig.PolicyConfig) PolicyConfigDTO {
	return PolicyConfigDTO{
		Enabled:              c.Enabled,
		TrackerLabel:         c.TrackerLabel,
		AutoFixOnCIFailure:   c.AutoFixOnCIFailure,
		MaxAutoFixRounds:     c.MaxAutoFixRounds,
		RequireAgentReview:   c.RequireAgentReview,
		ReviewStrategy:       c.ReviewStrategy,
		ReviewAgent:          c.ReviewAgent,
		MaxReviseRounds:      c.MaxReviseRounds,
		RequireHumanApproval: c.RequireHumanApproval,
		HumanTimeoutHours:    c.HumanTimeoutHours,
		AgentFinalPass:       c.AgentFinalPass,
		VetoSecondAgent:      c.VetoSecondAgent,
		MergeStrategy:        c.MergeStrategy,
		MinPRAgeMinutes:      c.MinPRAgeMinutes,
		BlockOnDraft:         c.BlockOnDraft,
	}
}

// PolicyConfigResponse is the body of GET/PUT /api/v1/projects/{id}/policy.
type PolicyConfigResponse struct {
	ProjectID string          `json:"projectId"`
	Config    PolicyConfigDTO `json:"config"`
}

// UpdatePolicyConfigRequest is the body of PUT /api/v1/projects/{id}/policy —
// a sparse diff merged onto policy.DefaultPolicyConfig (an omitted field keeps
// its default rather than zeroing). Sending an empty body resets the project's
// policy overrides back to the defaults wholesale (the `ao policy set --clear`
// path).
type UpdatePolicyConfigRequest struct {
	Enabled              *bool   `json:"enabled,omitempty"`
	TrackerLabel         *string `json:"trackerLabel,omitempty"`
	AutoFixOnCIFailure   *bool   `json:"autoFixOnCiFailure,omitempty"`
	MaxAutoFixRounds     *int    `json:"maxAutoFixRounds,omitempty"`
	RequireAgentReview   *bool   `json:"requireAgentReview,omitempty"`
	ReviewStrategy       *string `json:"reviewStrategy,omitempty"`
	ReviewAgent          *string `json:"reviewAgent,omitempty"`
	MaxReviseRounds      *int    `json:"maxReviseRounds,omitempty"`
	RequireHumanApproval *bool   `json:"requireHumanApproval,omitempty"`
	HumanTimeoutHours    *int    `json:"humanTimeoutHours,omitempty"`
	AgentFinalPass       *bool   `json:"agentFinalPass,omitempty"`
	VetoSecondAgent      *string `json:"vetoSecondAgent,omitempty"`
	MergeStrategy        *string `json:"mergeStrategy,omitempty"`
	MinPRAgeMinutes      *int    `json:"minPrAgeMinutes,omitempty"`
	BlockOnDraft         *bool   `json:"blockOnDraft,omitempty"`
}

// applyTo overlays the non-nil fields of the diff onto base and returns the
// result. base is expected to already be defaulted (see
// internalconfig.DefaultPolicyConfig / PolicyConfig.WithDefaults).
func (d UpdatePolicyConfigRequest) applyTo(base internalconfig.PolicyConfig) internalconfig.PolicyConfig {
	if d.Enabled != nil {
		base.Enabled = *d.Enabled
	}
	if d.TrackerLabel != nil {
		base.TrackerLabel = *d.TrackerLabel
	}
	if d.AutoFixOnCIFailure != nil {
		base.AutoFixOnCIFailure = *d.AutoFixOnCIFailure
	}
	if d.MaxAutoFixRounds != nil {
		base.MaxAutoFixRounds = *d.MaxAutoFixRounds
	}
	if d.RequireAgentReview != nil {
		base.RequireAgentReview = *d.RequireAgentReview
	}
	if d.ReviewStrategy != nil {
		base.ReviewStrategy = *d.ReviewStrategy
	}
	if d.ReviewAgent != nil {
		base.ReviewAgent = *d.ReviewAgent
	}
	if d.MaxReviseRounds != nil {
		base.MaxReviseRounds = *d.MaxReviseRounds
	}
	if d.RequireHumanApproval != nil {
		base.RequireHumanApproval = *d.RequireHumanApproval
	}
	if d.HumanTimeoutHours != nil {
		base.HumanTimeoutHours = *d.HumanTimeoutHours
	}
	if d.AgentFinalPass != nil {
		base.AgentFinalPass = *d.AgentFinalPass
	}
	if d.VetoSecondAgent != nil {
		base.VetoSecondAgent = *d.VetoSecondAgent
	}
	if d.MergeStrategy != nil {
		base.MergeStrategy = *d.MergeStrategy
	}
	if d.MinPRAgeMinutes != nil {
		base.MinPRAgeMinutes = *d.MinPRAgeMinutes
	}
	if d.BlockOnDraft != nil {
		base.BlockOnDraft = *d.BlockOnDraft
	}
	return base
}

// PolicyRunIDParam is the {runId} path parameter shared by /policy/runs routes.
type PolicyRunIDParam struct {
	RunID string `path:"runId" description:"Policy run identifier (uuid)."`
}

// GateResultDTO is one gate attempt in a policy run's history.
type GateResultDTO struct {
	RunID         string `json:"runId"`
	GateID        string `json:"gateId"`
	Attempt       int    `json:"attempt"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason,omitempty"`
	SecondVote    string `json:"secondVote,omitempty"`
	Justification string `json:"justification,omitempty"`
	DurationMS    int64  `json:"durationMs"`
}

func newGateResultDTO(g policy.GateResult) GateResultDTO {
	return GateResultDTO{
		RunID:         g.RunID,
		GateID:        string(g.GateID),
		Attempt:       g.Attempt,
		Outcome:       string(g.Outcome),
		Reason:        g.Reason,
		SecondVote:    g.SecondVote,
		Justification: g.Justification,
		DurationMS:    g.Duration.Milliseconds(),
	}
}

func newGateResultDTOs(in []policy.GateResult) []GateResultDTO {
	out := make([]GateResultDTO, 0, len(in))
	for _, g := range in {
		out = append(out, newGateResultDTO(g))
	}
	return out
}

// PolicyRunDTO is the wire shape of one policy run, returned by
// GET /api/v1/policy/runs/{runId}.
type PolicyRunDTO struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	SessionID   string          `json:"sessionId"`
	PRID        string          `json:"prId"`
	Config      PolicyConfigDTO `json:"config"`
	CurrentGate string          `json:"currentGate"`
	FinalState  string          `json:"finalState"`
	StartedAt   string          `json:"startedAt"`
	UpdatedAt   string          `json:"updatedAt"`
	History     []GateResultDTO `json:"history"`
}

// newPolicyRunDTO maps an engine Run onto its wire shape. The engine's Config
// snapshot is policy.Config, not internalconfig.PolicyConfig — the two mirror
// each other field-for-field (see internal/policy/config.go) but live in
// different packages by design, so the DTO conversion is explicit here.
func newPolicyRunDTO(run policy.Run) PolicyRunDTO {
	return PolicyRunDTO{
		ID:          run.ID,
		ProjectID:   run.ProjectID,
		SessionID:   run.SessionID,
		PRID:        run.PRID,
		Config:      newPolicyConfigDTO(policyEngineConfigToPersisted(run.Config)),
		CurrentGate: string(run.CurrentGate),
		FinalState:  run.FinalState,
		StartedAt:   run.StartedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   run.UpdatedAt.UTC().Format(time.RFC3339),
		History:     newGateResultDTOs(run.GateHistory),
	}
}

// policyEngineConfigToPersisted converts the engine's dependency-free
// policy.Config into the persisted internalconfig.PolicyConfig shape. The two
// types are intentionally kept structurally identical field-for-field (see
// doc comments on policy.Config and internalconfig.PolicyConfig) so the
// engine package stays free of the config/domain import chain.
func policyEngineConfigToPersisted(c policy.Config) internalconfig.PolicyConfig {
	return internalconfig.PolicyConfig{
		Enabled:              c.Enabled,
		TrackerLabel:         c.TrackerLabel,
		AutoFixOnCIFailure:   c.AutoFixOnCIFailure,
		MaxAutoFixRounds:     c.MaxAutoFixRounds,
		RequireAgentReview:   c.RequireAgentReview,
		ReviewStrategy:       c.ReviewStrategy,
		ReviewAgent:          c.ReviewAgent,
		MaxReviseRounds:      c.MaxReviseRounds,
		RequireHumanApproval: c.RequireHumanApproval,
		HumanTimeoutHours:    c.HumanTimeoutHours,
		AgentFinalPass:       c.AgentFinalPass,
		VetoSecondAgent:      c.VetoSecondAgent,
		MergeStrategy:        c.MergeStrategy,
		MinPRAgeMinutes:      c.MinPRAgeMinutes,
		BlockOnDraft:         c.BlockOnDraft,
	}
}

// PolicyRunGatesResponse is the body of GET /api/v1/policy/runs/{runId}/gates.
type PolicyRunGatesResponse struct {
	RunID string          `json:"runId"`
	Gates []GateResultDTO `json:"gates"`
}

// PolicyDecideRequest is the body of POST /api/v1/policy/runs/{runId}/decide.
type PolicyDecideRequest struct {
	Action        string `json:"action" enum:"approve,request_changes,override"`
	Justification string `json:"justification,omitempty"`
	Message       string `json:"message,omitempty"`
}

// toDecision converts the wire request into the engine's Decision type.
// Message (used for request_changes) is not part of policy.Decision today;
// Justification is the field both override and (future) audit trails read.
func (r PolicyDecideRequest) toDecision() policy.Decision {
	just := r.Justification
	if just == "" {
		just = r.Message
	}
	return policy.Decision{Action: r.Action, Justification: just}
}
