package deliverable

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type httpGetter interface {
	Get(url string) (*http.Response, error)
}

type webhookWatcher struct {
	logger *slog.Logger
	client *http.Client
}

func newWebhookWatcher(logger *slog.Logger) *webhookWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &webhookWatcher{
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (w *webhookWatcher) check(ctx context.Context, spec *domain.WebhookSpec) (bool, error) {
	if spec == nil {
		return false, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return false, err
	}

	if spec.AuthHeader != "" && spec.AuthHeaderValue != "" {
		req.Header.Set(spec.AuthHeader, spec.AuthHeaderValue)
	} else if spec.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+spec.BearerToken)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch spec.Condition {
	case "received":
		return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
	case "status_2xx":
		return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
	default:
		return false, fmt.Errorf("unknown webhook condition: %s", spec.Condition)
	}
}
