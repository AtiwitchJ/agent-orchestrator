package domain_test

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestParseCardStatus(t *testing.T) {
	got, err := domain.ParseCardStatus("ready")
	if err != nil || got != domain.CardStatusReady {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := domain.ParseCardStatus("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkboardConfigDefaults(t *testing.T) {
	d := domain.DefaultWorkboardConfig()
	if d.WIPLimit != 3 || d.AnswerTimeoutMinutes != 10 || d.LimitCooldownMinutes != 60 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.Autonomous.Enabled {
		t.Fatal("autonomous should default off")
	}
}
