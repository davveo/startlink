package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// Registry 多渠道注册表，业务可动态 Register 新渠道
type Registry struct {
	mu      sync.RWMutex
	senders map[domain.ChannelType]port.ChannelSender
}

func NewRegistry() *Registry {
	return &Registry{senders: make(map[domain.ChannelType]port.ChannelSender)}
}

func (r *Registry) Register(s port.ChannelSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senders[s.Channel()] = s
	slog.Info("channel registered", "channel", s.Channel())
}

func (r *Registry) Get(ch domain.ChannelType) (port.ChannelSender, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.senders[ch]
	if !ok {
		return nil, errcode.ChannelNotFound
	}
	return s, nil
}

func (r *Registry) List() []domain.ChannelType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.ChannelType, 0, len(r.senders))
	for k := range r.senders {
		out = append(out, k)
	}
	return out
}

// stubSender 本地联调：成功返回假 provider_id；可通过 Extra["force_fail"] 测不可重试失败
type stubSender struct {
	ch domain.ChannelType
}

func (s *stubSender) Channel() domain.ChannelType { return s.ch }

func (s *stubSender) Send(ctx context.Context, req domain.SendRequest) (*domain.SendResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if req.Extra != nil {
		if v, ok := req.Extra["force_fail"].(string); ok && v == "retryable" {
			return &domain.SendResult{Success: false, ErrorMsg: "stub retryable fail", Retryable: true}, nil
		}
		if v, ok := req.Extra["force_fail"].(string); ok && v == "permanent" {
			return &domain.SendResult{Success: false, ErrorMsg: "stub permanent fail", Retryable: false}, nil
		}
	}
	time.Sleep(2 * time.Millisecond)
	id := fmt.Sprintf("%s-%s-%d", s.ch, req.UserID, time.Now().UnixNano())
	slog.Debug("channel stub send", "channel", s.ch, "user", req.UserID, "title", req.Title, "provider_id", id)
	return &domain.SendResult{Success: true, Provider: string(s.ch), ProviderID: id}, nil
}

// httpSender POST SendRequest → SendResult；按 HTTP 状态映射 Retryable
type httpSender struct {
	ch     domain.ChannelType
	url    string
	client *http.Client
}

func NewHTTPSender(ch domain.ChannelType, url string, timeoutSec int) port.ChannelSender {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	return &httpSender{
		ch:     ch,
		url:    url,
		client: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

func (s *httpSender) Channel() domain.ChannelType { return s.ch }

const maxHTTPResponseBytes = 1 << 20 // 1 MiB

func (s *httpSender) Send(ctx context.Context, req domain.SendRequest) (*domain.SendResult, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return &domain.SendResult{Success: false, ErrorMsg: err.Error(), Retryable: false}, nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(raw))
	if err != nil {
		return &domain.SendResult{Success: false, ErrorMsg: err.Error(), Retryable: false}, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return &domain.SendResult{Success: false, ErrorMsg: err.Error(), Retryable: true}, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBytes+1))
	if err != nil {
		return &domain.SendResult{Success: false, ErrorMsg: err.Error(), Retryable: true}, nil
	}
	if len(body) > maxHTTPResponseBytes {
		return &domain.SendResult{
			Success:   false,
			ErrorMsg:  fmt.Sprintf("channel http response too large (>%d bytes)", maxHTTPResponseBytes),
			Retryable: false,
		}, nil
	}

	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return &domain.SendResult{
			Success:   false,
			ErrorMsg:  fmt.Sprintf("http %d: %s", resp.StatusCode, trunc(string(body), 200)),
			Retryable: true,
			Throttled: resp.StatusCode == 429,
		}, nil
	}
	if resp.StatusCode >= 400 {
		return &domain.SendResult{
			Success:   false,
			ErrorMsg:  fmt.Sprintf("http %d: %s", resp.StatusCode, trunc(string(body), 200)),
			Retryable: false,
		}, nil
	}

	var result domain.SendResult
	if len(body) > 0 {
		if err := json.Unmarshal(body, &result); err != nil {
			// 2xx 但非 JSON：视为成功，用响应体前缀作 provider_id
			return &domain.SendResult{
				Success:    true,
				Provider:   string(s.ch),
				ProviderID: trunc(string(body), 64),
			}, nil
		}
	}
	if result.Provider == "" {
		result.Provider = string(s.ch)
	}
	if result.ProviderID == "" && result.Success {
		result.ProviderID = fmt.Sprintf("%s-%s-%d", s.ch, req.UserID, time.Now().UnixNano())
	}
	return &result, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func NewAppPush() port.ChannelSender  { return &stubSender{ch: domain.ChannelAppPush} }
func NewSMS() port.ChannelSender      { return &stubSender{ch: domain.ChannelSMS} }
func NewEmail() port.ChannelSender    { return &stubSender{ch: domain.ChannelEmail} }
func NewInbox() port.ChannelSender    { return &stubSender{ch: domain.ChannelInbox} }
func NewWecom() port.ChannelSender    { return &stubSender{ch: domain.ChannelWecom} }
func NewDingtalk() port.ChannelSender { return &stubSender{ch: domain.ChannelDingtalk} }

func RegisterDefaults(reg *Registry) {
	reg.Register(NewAppPush())
	reg.Register(NewSMS())
	reg.Register(NewEmail())
	reg.Register(NewInbox())
	reg.Register(NewWecom())
	reg.Register(NewDingtalk())
}

// RegisterFromConfig 按 pusher.channels 覆盖为 HTTP 发送器（mode=http 且 url 非空）
func RegisterFromConfig(reg *Registry, channels map[string]config.ChannelSenderConfig) {
	for name, cfg := range channels {
		ch := domain.ChannelType(name)
		if !ch.Valid() {
			slog.Warn("skip invalid channel config", "channel", name)
			continue
		}
		mode := cfg.Mode
		if mode == "" {
			mode = "stub"
		}
		if mode == "http" && cfg.URL != "" {
			reg.Register(NewHTTPSender(ch, cfg.URL, cfg.TimeoutSec))
			slog.Info("channel http sender configured",
				"channel", ch,
				"timeout_sec", cfg.TimeoutSec,
				"max_retry", cfg.MaxRetry,
				"retry_backoff", cfg.RetryBackoff,
			)
		}
	}
}
