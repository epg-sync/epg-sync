package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/epg-sync/epgsync/pkg/logger"
	"github.com/gin-gonic/gin"
)

// serveLogged 用挂了 RequestLogger 的路由处理一个请求，返回记下的那条日志
func serveLogged(t *testing.T, req *http.Request) logger.Record {
	t.Helper()
	gin.SetMode(gin.TestMode)

	hub := logger.NewHub(10)
	l, err := logger.NewLogger(&logger.Config{Level: "debug", Format: "json", Output: "stdout"}, hub.Core(logger.DebugLevel))
	if err != nil {
		t.Fatal(err)
	}
	prev := logger.Default()
	logger.SetDefault(l)
	t.Cleanup(func() { logger.SetDefault(prev) })

	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/epg", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.ServeHTTP(httptest.NewRecorder(), req)

	select {
	case r := <-hub.Records():
		return r
	default:
		t.Fatal("no request log recorded")
		return logger.Record{}
	}
}

func TestRequestLoggerPrefersCFConnectingIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/epg", nil)
	req.RemoteAddr = "172.68.1.1:443"
	req.Header.Set("X-Forwarded-For", "172.68.1.1")
	req.Header.Set("CF-Connecting-IP", "203.0.113.7")

	r := serveLogged(t, req)
	if got := r.Fields["ip"]; got != "203.0.113.7" {
		t.Errorf("ip = %#v, want CF-Connecting-IP 203.0.113.7", got)
	}
}

func TestRequestLoggerFallsBackWithoutCFHeader(t *testing.T) {
	for name, cfIP := range map[string]string{"missing": "", "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/epg", nil)
			req.RemoteAddr = "198.51.100.20:5555"
			if cfIP != "" {
				req.Header.Set("CF-Connecting-IP", cfIP)
			}

			r := serveLogged(t, req)
			if got := r.Fields["ip"]; got != "198.51.100.20" {
				t.Errorf("ip = %#v, want RemoteAddr 198.51.100.20", got)
			}
		})
	}
}

func TestRequestLoggerRecordsUserAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/epg", nil)
	req.Header.Set("User-Agent", "TiviMate/5.1.6")

	r := serveLogged(t, req)
	if got := r.Fields["user_agent"]; got != "TiviMate/5.1.6" {
		t.Errorf("user_agent = %#v, want TiviMate/5.1.6", got)
	}
}
