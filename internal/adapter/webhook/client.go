package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
)

// Client 终态 Webhook 通知
type Client struct {
	httpClient *http.Client
	defaultURL string
	enabled    bool
	allowHTTP  bool
	allowed    map[string]struct{}
	secret     []byte
	sem        chan struct{}
}

func New(cfg config.WebhookConfig) *Client {
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 5
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 32
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedHosts)+1)
	for _, host := range cfg.AllowedHosts {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			allowed[host] = struct{}{}
		}
	}
	if u, err := url.Parse(cfg.DefaultURL); err == nil && u.Hostname() != "" {
		allowed[strings.ToLower(u.Hostname())] = struct{}{}
	}
	c := &Client{
		defaultURL: cfg.DefaultURL,
		enabled:    cfg.Enabled,
		allowHTTP:  cfg.AllowHTTP,
		allowed:    allowed,
		secret:     []byte(cfg.SigningSecret),
		sem:        make(chan struct{}, cfg.MaxConcurrent),
	}
	c.httpClient = &http.Client{
		Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return c.validateURL(req.URL.String())
		},
	}
	return c
}

func (c *Client) ResolveURL(taskURL string) string {
	if taskURL != "" {
		return taskURL
	}
	return c.defaultURL
}

func (c *Client) ValidateTarget(taskURL string) error {
	target := c.ResolveURL(taskURL)
	if target == "" || !c.enabled {
		return nil
	}
	return c.validateURL(target)
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
	if err := c.validateURL(target); err != nil {
		return err
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
	if len(c.secret) >= 32 {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		mac := hmac.New(sha256.New, c.secret)
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("\n"))
		_, _ = mac.Write(body)
		req.Header.Set("X-Starlink-Timestamp", timestamp)
		req.Header.Set("X-Starlink-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	slog.Info("webhook notified", "url", target, "task_id", event.TaskID, "status", event.Status)
	return nil
}

func (c *Client) validateURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return fmt.Errorf("invalid webhook url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(c.allowHTTP && scheme == "http") {
		return fmt.Errorf("webhook scheme not allowed")
	}
	if _, ok := c.allowed[strings.ToLower(u.Hostname())]; !ok {
		return fmt.Errorf("webhook host not allowed")
	}
	return nil
}

// NotifyAsync 异步通知，不阻塞主流程
func (c *Client) NotifyAsync(url string, event domain.WebhookEvent) {
	select {
	case c.sem <- struct{}{}:
	case <-time.After(100 * time.Millisecond):
		slog.Warn("webhook dropped: concurrency limit", "task_id", event.TaskID)
		return
	}
	go func() {
		defer func() { <-c.sem }()
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = c.NotifyTaskFinished(ctx, url, event)
			cancel()
			if err == nil {
				return
			}
			time.Sleep(time.Duration(1<<attempt) * 200 * time.Millisecond)
		}
		slog.Warn("webhook failed", "task_id", event.TaskID, "err", err)
	}()
}
