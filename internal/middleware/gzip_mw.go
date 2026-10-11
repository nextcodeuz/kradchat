package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
)

// gzipWriterPool — har so'rovda yangi gzip.Writer yaratmaslik uchun.
// Daraja BestSpeed: TTFB past, nisbat yetarli (wasm ~3x kichrayadi).
var gzipWriterPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return w
}}

// gzipExts — siqish arziydigan statik kengaytmalar (matnli + wasm).
// Rasmlar/video/zip allaqachon siqilgan — teginmaymiz.
var gzipExts = map[string]bool{
	".wasm": true, ".js": true, ".mjs": true, ".css": true,
	".html": true, ".json": true, ".svg": true, ".txt": true,
	".map": true, ".xml": true,
}

// GzipStatic — statik javoblarni gzip'laydi (5MB wasm ~1.7MB bo'ladi).
// Faqat GET, Accept-Encoding: gzip, mos kengaytma, Range'siz so'rovlar.
// HEAD'da body yo'q — siqishdan ma'no yo'q (Content-Length saqlanadi).
// FileServer ETag/Last-Modified'ni o'zi boshqaradi; biz Content-Length'ni
// o'chiramiz (chunked) va Vary qo'shamiz. Range bo'lsa siqmaymiz
// (qisman kontent + gzip birga ishlamaydi).
func GzipStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if !gzipExts[strings.ToLower(path.Ext(r.URL.Path))] {
			next.ServeHTTP(w, r)
			return
		}
		gw := gzipWriterPool.Get().(*gzip.Writer)
		defer gzipWriterPool.Put(gw)
		// Muhim: gw underlying writer ASL ResponseWriter bo'lishi kerak
		// (gzw emas — aks holda Write→gw→gzw→gw cheksiz rekursiya).
		gw.Reset(w)
		defer gw.Close()
		gzw := &gzipResponseWriter{ResponseWriter: w, gw: gw}
		h := w.Header()
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		next.ServeHTTP(gzw, r)
	})
}

// gzipResponseWriter — Write'larni gzip oqimiga yo'naltiradi.
// Header yozilishidan oldin 304/204 bo'lsa siqilmaydi (WriteHeader hook).
type gzipResponseWriter struct {
	http.ResponseWriter
	gw          *gzip.Writer
	wroteHeader bool
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	// 304/204/HEAD javoblarida body yo'q — encoding header'ni tozalaymiz.
	if status == http.StatusNotModified || status == http.StatusNoContent {
		h := g.ResponseWriter.Header()
		h.Del("Content-Encoding")
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	return g.gw.Write(b)
}

// Flush — SSE/proksi uchun (FileServer'da shart emas, interfeys to'liqligi).
func (g *gzipResponseWriter) Flush() {
	_ = g.gw.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
