//go:build js && wasm

package main

import (
	"syscall/js"
)

// Toast — qisqa ogohlantirish/xabar.
var toastTimer js.Value

func showToast(n *Notif) {
	toast := ByID("toast")
	if toast.IsNull() || toast.IsUndefined() {
		return
	}
	if n == nil {
		n = &Notif{Type: "info", Title: "Xabar"}
	}
	toast.Get("dataset").Set("type", n.Type)
	title := El("div", "toast-title")
	SetText(title, n.Title)
	body := El("div", "toast-body")
	SetText(body, n.Body)
	HTML(toast, "")
	Append(toast, title)
	Append(toast, body)
	AddClass(toast, "show")

	if !toastTimer.IsUndefined() {
		js.Global().Call("clearTimeout", toastTimer)
	}
	toastTimer = js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
		RemoveClass(toast, "show")
		return nil
	}), 3000)
}
