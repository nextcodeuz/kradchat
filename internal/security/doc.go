// Package security — kirish validatsiyasi, XSS sanitizer, rate-limit,
// dedup (takrorlanmas xabar) primitivlari.
//
// Qatlam: leaf — ichki paketlarga bog'lanmaydi, hamma qatlamdan chaqiriladi.
// Fail-open tamoyili: himoya o'zi availability'ni buzmasligi kerak
// (dedup to'lganda evict, bloklamaydi).
package security
