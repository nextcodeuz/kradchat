// Package service — biznes logika (Handler → Service → Repository).
//
// Qatlam: handler ostida, repository/ws ustida.
// Tarkib: auth (+pending/device/directory), twofa, chat (persist-then-broadcast),
// upload (chunked + registry + orphan GC), presence (copy-on-write snapshot),
// background (auto-scan), block.
// Lifecycle: Start(ctx) fon goroutinalarni ishga tushiradi, Stop() idempotent
// to'xtatadi (double-close panic yo'q).
package service
