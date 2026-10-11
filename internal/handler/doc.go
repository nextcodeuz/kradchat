// Package handler — HTTP/WS transport: routing, DTO, JSON, limitlar.
//
// Qatlam: eng tashqi — service, middleware, ws dan foydalanadi;
// repository'ga faqat xato-sentinel mapping uchun tayanadi (biznes chaqiruv yo'q).
// Composition: NewRouter + Handler(); middleware zanjiri
// Logging → RateLimit → CORS → SecurityHeaders tartibida o'raladi.
package handler
