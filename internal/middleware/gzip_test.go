package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipTestHandler(body string) http.Handler {
	return GzipStatic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		_, _ = w.Write([]byte(body))
	}))
}

func gunzip(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return string(out)
}

// TestGzipStaticCompresses — mos kengaytma + Accept-Encoding: gzip → siqiladi.
func TestGzipStaticCompresses(t *testing.T) {
	body := strings.Repeat("wasm-bytes-0123456789", 5000)
	r := httptest.NewRequest(http.MethodGet, "/wasm/app.wasm", nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate")
	w := httptest.NewRecorder()
	gzipTestHandler(body).ServeHTTP(w, r)

	res := w.Result()
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding=%q, want gzip", res.Header.Get("Content-Encoding"))
	}
	if res.Header.Get("Content-Length") != "" {
		t.Fatalf("Content-Length saqlanmasligi kerak (chunked): %q", res.Header.Get("Content-Length"))
	}
	if vary := res.Header.Get("Vary"); !strings.Contains(vary, "Accept-Encoding") {
		t.Fatalf("Vary=%q", vary)
	}
	if got := gunzip(t, w.Body.Bytes()); got != body {
		t.Fatal("roundtrip buzildi")
	}
	if w.Body.Len() >= len(body) {
		t.Fatalf("siqilmadi: %d >= %d", w.Body.Len(), len(body))
	}
}

// TestGzipStaticSkips — gzip so'ralmasa / kengaytma mos bo'lmasa / Range
// bo'lsa — tegilmaydi.
func TestGzipStaticSkips(t *testing.T) {
	body := strings.Repeat("x", 1000)
	cases := []struct {
		name   string
		path   string
		method string
		header map[string]string
	}{
		{"encoding-yoq", "/wasm/app.wasm", http.MethodGet, nil},
		{"rasm", "/static/logo.png", http.MethodGet, map[string]string{"Accept-Encoding": "gzip"}},
		{"range", "/wasm/app.wasm", http.MethodGet, map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-99"}},
		{"post", "/wasm/app.wasm", http.MethodPost, map[string]string{"Accept-Encoding": "gzip"}},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		for k, v := range c.header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		gzipTestHandler(body).ServeHTTP(w, r)
		if enc := w.Result().Header.Get("Content-Encoding"); enc != "" {
			t.Errorf("%s: Content-Encoding=%q, bo'sh bo'lishi kerak", c.name, enc)
		}
		if w.Body.String() != body {
			t.Errorf("%s: body o'zgardi", c.name)
		}
	}
}
