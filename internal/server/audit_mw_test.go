package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/domain"
)

type recordingAuditRepo struct {
	mu      sync.Mutex
	entries []domain.AuditLog
	written chan struct{}
}

func newRecordingAuditRepo() *recordingAuditRepo {
	return &recordingAuditRepo{written: make(chan struct{}, 16)}
}

func (r *recordingAuditRepo) Create(_ context.Context, log *domain.AuditLog) error {
	r.mu.Lock()
	r.entries = append(r.entries, *log)
	r.mu.Unlock()
	r.written <- struct{}{}
	return nil
}

func (r *recordingAuditRepo) List(context.Context, domain.ListAuditLogQuery) ([]domain.AuditLog, int64, error) {
	return nil, 0, nil
}

func (r *recordingAuditRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func auditEngine(repo *recordingAuditRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware(repo))
	r.POST("/api/v1/callbacks/receipt", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/campaigns", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/campaigns", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func post(engine *gin.Engine, path string) {
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
}

// 渠道回执与发送量同级，不能进审计表。
func TestAuditMiddlewareSkipsChannelCallbacks(t *testing.T) {
	repo := newRecordingAuditRepo()
	engine := auditEngine(repo)

	post(engine, "/api/v1/callbacks/receipt")
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/campaigns", nil))

	select {
	case <-repo.written:
		t.Fatalf("callback/query traffic must not be audited, got %d entries", repo.count())
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAuditMiddlewareRecordsOperatorWrites(t *testing.T) {
	repo := newRecordingAuditRepo()
	post(auditEngine(repo), "/api/v1/campaigns")

	select {
	case <-repo.written:
	case <-time.After(2 * time.Second):
		t.Fatal("expected an audit entry for the campaign write")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if got := repo.entries[0].Action; got != "campaign.create" {
		t.Fatalf("unexpected audit action %q", got)
	}
}
