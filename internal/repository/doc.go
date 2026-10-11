// Package repository — SQL saqlash qatlami (3 dialekt).
//
// Qatlam: service → repository → database (config, model'ga bog'lanadi).
// Qoidalar:
//   - hamma so'rov parametrlangan (string interpolatsiya taqiqlanadi);
//   - driver xatolar wrapErr orqali ErrNotFound/ErrConflict ga normalanadi;
//   - UPDATE/DELETE 0 qatorga tegsa ErrNotFound (silent no-op yo'q);
//   - ro'yxat so'rovlarida rows.Err() tekshiriladi.
package repository
