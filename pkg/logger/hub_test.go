package logger

import (
	"errors"
	"io"
	"testing"
	"time"
)

func newHubLogger(t *testing.T, size int, level string) (Logger, *Hub) {
	t.Helper()
	hub := NewHub(size)
	l, err := NewLogger(&Config{Level: level, Format: "json", Output: "stdout", EnableCaller: true}, hub.Core(ParseLevel(level)))
	if err != nil {
		t.Fatal(err)
	}
	return l, hub
}

func TestHubCapturesStructuredEntries(t *testing.T) {
	// stdout 的输出不关心，丢弃
	l, hub := newHubLogger(t, 10, "info")
	l = l.Named("http").With(String("component", "router"))
	l.Warn("request failed",
		Int("status", 502),
		Err(errors.New("upstream timeout")),
		Duration("latency", 1500*time.Millisecond),
	)
	l.Debug("ignored below level")

	select {
	case r := <-hub.Records():
		if r.Message != "request failed" || r.Level != WarnLevel || r.Logger != "http" {
			t.Errorf("unexpected record %+v", r)
		}
		if r.Caller == "" {
			t.Error("caller not recorded")
		}
		want := map[string]any{"component": "router", "status": int64(502), "error": "upstream timeout", "latency": "1.5s"}
		for k, v := range want {
			if r.Fields[k] != v {
				t.Errorf("field %s = %#v, want %#v", k, r.Fields[k], v)
			}
		}
	default:
		t.Fatal("no record published")
	}

	select {
	case r := <-hub.Records():
		t.Errorf("debug entry should be filtered, got %+v", r)
	default:
	}
}

func TestHubDropsWhenFull(t *testing.T) {
	l, hub := newHubLogger(t, 2, "info")
	for i := 0; i < 5; i++ {
		l.Info("x")
	}
	if got := hub.Dropped(); got != 3 {
		t.Errorf("dropped = %d, want 3", got)
	}
	if got := len(hub.Records()); got != 2 {
		t.Errorf("buffered = %d, want 2", got)
	}
}

func TestJSONSafeFallsBack(t *testing.T) {
	got := jsonSafe(map[string]any{"ch": make(chan int), "ok": 1, "w": io.Discard})
	if _, isString := got["ch"].(string); !isString {
		t.Errorf("unencodable value not converted: %#v", got["ch"])
	}
	if got["ok"] != 1 {
		t.Errorf("encodable value changed: %#v", got["ok"])
	}
}
