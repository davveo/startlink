package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/starlink/push/internal/domain"
)

// Client 终态 Webhook 通知
type Client struct {
	httpClient *http.Client
	defaultURL string
	enabled    bool
}

func New(defaultURL string, timeoutSec int, enabled bool) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	return &Client{
		httpClient: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		defaultURL: defaultURL,
		enabled:    enabled,
	}
}

func (c *Client) ResolveURL(taskURL string) string {
	if taskURL != "" {
		return taskURL
	}
	return c.defaultURL
}

func (c *Client) NotifyTaskFinished(ctx context.Context, url string, event domain.WebhookEvent) error {
	if !c.enabled {
		return nil
	}
	target := url
	if target == "" {
		target = c.defaultURL
	}
	if target == "" {
		return nil
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Starlink-Event", event.Event)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	slog.Info("webhook notified", "url", target, "task_id", event.TaskID, "status", event.Status)
	return nil
}

// NotifyAsync 异步通知，不阻塞主流程
func (c *Client) NotifyAsync(url string, event domain.WebhookEvent) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.NotifyTaskFinished(ctx, url, event); err != nil {
			slog.Warn("webhook failed", "task_id", event.TaskID, "err", err)
		}
	}()
}
