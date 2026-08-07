package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCORSEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(corsMiddleware([]string{"http://localhost:5173"}))
	r.POST("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func doPost(t *testing.T, host, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ping", nil)
	req.Host = host
	req.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	newCORSEngine().ServeHTTP(w, req)
	return w
}

// 浏览器对同源写请求同样带 Origin；控制台可能被从 127.0.0.1 或内网地址打开，
// 这些 origin 不会出现在 allowed_origins 里，但必须放行。
func TestCORSAllowsSameOriginOutsideAllowlist(t *testing.T) {
	w := doPost(t, "127.0.0.1:3000", "http://127.0.0.1:3000")
	if w.Code != http.StatusOK {
		t.Fatalf("same-origin request must pass, got %d", w.Code)
	}
}

func TestCORSAllowsSameOriginBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ping", nil)
	req.Host = "ops.example.com"
	req.Header.Set("Origin", "https://ops.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	newCORSEngine().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxied same-origin request must pass, got %d", w.Code)
	}
}

func TestCORSAllowsListedCrossOrigin(t *testing.T) {
	w := doPost(t, "localhost:8080", "http://localhost:5173")
	if w.Code != http.StatusOK {
		t.Fatalf("allowlisted origin must pass, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("missing CORS origin header, got %q", got)
	}
}

func TestCORSRejectsUnknownCrossOrigin(t *testing.T) {
	w := doPost(t, "localhost:8080", "http://evil.example.com")
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request must be rejected, got %d", w.Code)
	}
}
