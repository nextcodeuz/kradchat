// Package ws — in-memory WebSocket Hub/Client/Room (Redis siz).
//
// Qatlam: transport — service Hub'dan broadcast uchun foydalanadi
// (on-process message bus; klaster rejimda almashtiriladi).
// Qoidalar: lock tartibi hub→room→client; broadcast snapshot olib yuboriladi;
// bo'sh non-global xonalar tozalanadi; yagona tozalash mexanizmi Done() kanali.
package ws
