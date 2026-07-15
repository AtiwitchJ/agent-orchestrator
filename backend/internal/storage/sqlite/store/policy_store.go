package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/policy"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

var _ policy.Store = (*Store)(nil)

// CreateRun persists a new policy run row. run.Config is serialized to the
// config_snapshot JSON column; run.GateHistory is ignored (gate attempts are
// recorded separately via RecordGateResult).
func (s *Store) CreateRun(ctx context.Context, run policy.Run) error {
	snapshot, err := json.Marshal(run.Config)
	if err != nil {
		return fmt.Errorf("marshal policy config snapshot for run %s: %w", run.ID, err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.qw.CreatePolicyRun(ctx, gen.CreatePolicyRunParams{
		ID:             run.ID,
		ProjectID:      run.ProjectID,
		SessionID:      run.SessionID,
		PRID:           run.PRID,
		ConfigSnapshot: string(snapshot),
		CurrentGate:    string(run.CurrentGate),
		StartedAt:      unixFromTime(run.StartedAt),
		UpdatedAt:      unixFromTime(run.UpdatedAt),
	})
	if err != nil {
		return fmt.Errorf("create policy run %s: %w", run.ID, err)
	}
	return nil
}

// GetRun returns the run row (GateHistory left nil; see ListGateResults),
// ok=false when runID is unknown.
func (s *Store) GetRun(ctx context.Context, runID string) (policy.Run, bool, error) {
	row, err := s.qr.GetPolicyRun(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return policy.Run{}, false, nil
	}
	if err != nil {
		return policy.Run{}, false, fmt.Errorf("get policy run %s: %w", runID, err)
	}
	run, err := policyRunFromRow(row)
	if err != nil {
		return policy.Run{}, false, fmt.Errorf("decode policy run %s: %w", runID, err)
	}
	return run, true, nil
}

// ListActiveRuns returns every run with an empty FinalState, oldest updated
// first, for boot-time recovery of in-flight runs.
func (s *Store) ListActiveRuns(ctx context.Context) ([]policy.Run, error) {
	rows, err := s.qr.ListActivePolicyRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active policy runs: %w", err)
	}
	out := make([]policy.Run, 0, len(rows))
	for _, row := range rows {
		run, err := policyRunFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode policy run %s: %w", row.ID, err)
		}
		out = append(out, run)
	}
	return out, nil
}

// UpdateCurrentGate advances the persisted current-gate cache column.
func (s *Store) UpdateCurrentGate(ctx context.Context, runID string, gate policy.GateID, updatedAt time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.UpdatePolicyRunGate(ctx, gen.UpdatePolicyRunGateParams{
		CurrentGate: string(gate),
		UpdatedAt:   unixFromTime(updatedAt),
		ID:          runID,
	}); err != nil {
		return fmt.Errorf("update current gate for run %s: %w", runID, err)
	}
	return nil
}

// FinalizeRun sets the terminal state cache column.
func (s *Store) FinalizeRun(ctx context.Context, runID, finalState string, updatedAt time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.FinalizePolicyRun(ctx, gen.FinalizePolicyRunParams{
		FinalState: sql.NullString{String: finalState, Valid: finalState != ""},
		UpdatedAt:  unixFromTime(updatedAt),
		ID:         runID,
	}); err != nil {
		return fmt.Errorf("finalize policy run %s: %w", runID, err)
	}
	return nil
}

// RecordGateResult appends one gate-attempt row.
func (s *Store) RecordGateResult(ctx context.Context, result policy.GateResult) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.qw.RecordGateResult(ctx, gen.RecordGateResultParams{
		ID:            gateResultID(result),
		RunID:         result.RunID,
		GateID:        string(result.GateID),
		Attempt:       int64(result.Attempt),
		Outcome:       string(result.Outcome),
		Reason:        sql.NullString{String: result.Reason, Valid: result.Reason != ""},
		SecondVote:    sql.NullString{String: result.SecondVote, Valid: result.SecondVote != ""},
		Justification: sql.NullString{String: result.Justification, Valid: result.Justification != ""},
		DurationMs:    sql.NullInt64{Int64: result.Duration.Milliseconds(), Valid: result.Duration > 0},
		CreatedAt:     time.Now().UTC().Unix(),
	})
	if err != nil {
		return fmt.Errorf("record gate result for run %s gate %s attempt %d: %w", result.RunID, result.GateID, result.Attempt, err)
	}
	return nil
}

// ListGateResults returns every attempt for a run, oldest first.
func (s *Store) ListGateResults(ctx context.Context, runID string) ([]policy.GateResult, error) {
	rows, err := s.qr.ListGateResults(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list gate results for run %s: %w", runID, err)
	}
	out := make([]policy.GateResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, gateResultFromRow(row))
	}
	return out, nil
}

func policyRunFromRow(row gen.PolicyRun) (policy.Run, error) {
	var cfg policy.Config
	if err := json.Unmarshal([]byte(row.ConfigSnapshot), &cfg); err != nil {
		return policy.Run{}, fmt.Errorf("unmarshal config_snapshot: %w", err)
	}
	return policy.Run{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		SessionID:   row.SessionID,
		PRID:        row.PRID,
		Config:      cfg,
		CurrentGate: policy.GateID(row.CurrentGate),
		FinalState:  row.FinalState.String,
		StartedAt:   time.Unix(row.StartedAt, 0).UTC(),
		UpdatedAt:   time.Unix(row.UpdatedAt, 0).UTC(),
	}, nil
}

func gateResultFromRow(row gen.GateResult) policy.GateResult {
	return policy.GateResult{
		RunID:         row.RunID,
		GateID:        policy.GateID(row.GateID),
		Attempt:       int(row.Attempt),
		Outcome:       policy.GateOutcome(row.Outcome),
		Reason:        row.Reason.String,
		SecondVote:    row.SecondVote.String,
		Justification: row.Justification.String,
		Duration:      time.Duration(row.DurationMs.Int64) * time.Millisecond,
	}
}

// gateResultID derives a stable id for one gate attempt row. gate_results has
// no natural caller-supplied id (unlike policy_runs); run+gate+attempt is
// already unique per idx_gate_results_run, so reuse it as the primary key
// rather than plumbing a new UUID dependency into the store layer.
func gateResultID(r policy.GateResult) string {
	return fmt.Sprintf("%s:%s:%d", r.RunID, r.GateID, r.Attempt)
}

func unixFromTime(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().UTC().Unix()
	}
	return t.UTC().Unix()
}
