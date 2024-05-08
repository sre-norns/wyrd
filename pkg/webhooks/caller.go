package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sre-norns/wyrd/pkg/bark"
)

type Caller interface {
	Post(ctx context.Context, hook Webhook, event EventPayload) error
}

type HTTPCaller struct {
	client *http.Client
}

func NewHTTPCaller(client *http.Client) (Caller, error) {
	return &HTTPCaller{
		client: client,
	}, nil
}

func (h *HTTPCaller) Post(ctx context.Context, hook Webhook, event EventPayload) error {
	targetUtl, err := hook.Spec.TargetURL()
	if err != nil {
		return fmt.Errorf("failed to build a target URL from webhook Spec: %w", err)
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	if err := encoder.Encode(event); err != nil {
		return fmt.Errorf("failed to serialize webhook payload body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetUtl.String(), &buffer)
	if err != nil {
		return fmt.Errorf("failed to create a new POST request: %w", err)
	}
	req.Header.Set(bark.HTTPHeaderContentType, bark.MimeTypeJSON)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to POST webhook %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated, http.StatusNoContent, http.StatusAlreadyReported:
		return nil
	default:
		return fmt.Errorf("webhook POST was unsuccessful: %v", resp.Status)
	}
}
