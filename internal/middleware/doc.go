// Package middleware — HTTP kesishmalari: auth, rate-limit, security headers,
// CORS, logging, client IP.
//
// Qatlam: infra — service'ning faqat AuthService validatsiyasidan foydalanadi
// (ichkariga bog'lanish; teskari yo'nalish taqiqlanadi: service hech qachon
// middleware/handler import qilmaydi).
package middleware
