// Package database — DB ulanish pool'i va schema migratsiya.
//
// Qatlam: config → database (faqat config'ga bog'lanadi).
// Uch dialekt (postgres/mysql/sqlite) `database/sql` ustidan.
// Migrate har startda idempotent ishlaydi (CREATE TABLE IF NOT EXISTS);
// eski bazalarga ustun/jadval `ensureColumns` orqali yetkaziladi.
package database
