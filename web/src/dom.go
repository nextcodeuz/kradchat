//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"
)

// js.Value yordamchilari — DOM manipulyatsiya uchun minimal o'ram.

var document = js.Global().Get("document")

// Q — selector bo'yicha element qidiradi.
func Q(sel string) js.Value {
	return document.Call("querySelector", sel)
}

// QAll — selector bo'yicha elementlar ro'yxati.
func QAll(sel string) []js.Value {
	all := document.Call("querySelectorAll", sel)
	n := all.Length()
	out := make([]js.Value, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, all.Index(i))
	}
	return out
}

// El — yangi element yaratadi.
func El(tag, class string) js.Value {
	el := document.Call("createElement", tag)
	if class != "" {
		el.Set("className", class)
	}
	return el
}

// ByID — id bo'yicha element.
func ByID(id string) js.Value {
	return document.Call("getElementById", id)
}

// SetText — textContent o'rnatadi (XSS xavfsiz).
func SetText(el js.Value, text string) {
	el.Set("textContent", text)
}

// HTML — innerHTML o'rnatadi (faqat ishonchli render uchun).
func HTML(el js.Value, html string) {
	el.Set("innerHTML", html)
}

// Show/SetDisplay — element ko'rinishini boshqaradi.
func Show(el js.Value) {
	el.Get("style").Set("display", "block")
}

func Hide(el js.Value) {
	el.Get("style").Set("display", "none")
}

// AddClass / RemoveClass — classList DOMTokenList obyekti, elementning
// methodi EMAS: el.Get("classList").Call("add", ...) to'g'ri yozuv.
func AddClass(el js.Value, class string) {
	el.Get("classList").Call("add", class)
}

func RemoveClass(el js.Value, class string) {
	el.Get("classList").Call("remove", class)
}

// Attr — atribut o'rnatadi (setAttribute — property emas).
// Avval el.Set(name, value) edi — bu property yozardi, atribut emas.
func Attr(el js.Value, name, value string) {
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Call("setAttribute", name, value)
}

// On — hodisa qo'shadi. js.FuncOf instance'ini xotirada saqlaydi.
// Panic'lar toza tutib toast orqali ko'rsatiladi — aks holda Go WASM
// JS muhitida hech qanday xabar bermay jim o'chib ketadi (barcha tugmalar
// ishlamay qolardi, sabab esa ko'rinmasdi).
func On(el js.Value, event string, fn func(this js.Value, args []js.Value)) {
	f := js.FuncOf(func(this js.Value, args []js.Value) any {
		defer func() {
			if r := recover(); r != nil {
				showToast(&Notif{Type: "error", Title: "Xatolik", Body: fmt.Sprint(r)})
			}
		}()
		fn(this, args)
		return nil
	})
	el.Call("addEventListener", event, f)
	keptFuncs = append(keptFuncs, f)
}

var keptFuncs []js.Func

// GetVal — input/select qiymatini oladi.
func GetVal(el js.Value) string {
	return el.Get("value").String()
}

// SetVal — input/select qiymatini o'rnatadi.
func SetVal(el js.Value, v string) {
	el.Set("value", v)
}

// Checked — checkbox holati.
func IsChecked(el js.Value) bool {
	return el.Get("checked").Bool()
}

func SetChecked(el js.Value, checked bool) {
	el.Set("checked", checked)
}

// AppendChild
func Append(parent, child js.Value) {
	parent.Call("appendChild", child)
}

func Remove(el js.Value) {
	parent := el.Get("parentNode")
	if !parent.IsNull() && !parent.IsUndefined() {
		parent.Call("removeChild", el)
	}
}

// GetData / SetData — dataset atributlari.
func GetData(el js.Value, key string) string {
	return el.Get("dataset").Get(key).String()
}

func SetData(el js.Value, key, value string) {
	el.Get("dataset").Set(key, value)
}

// ScrollBottom — elementni pastga scroll qiladi.
func ScrollBottom(el js.Value) {
	el.Set("scrollTop", el.Get("scrollHeight"))
}

// isNearBottom — foydalanuvchi elementning pastki qismida turganini tekshiradi.
// Auto-scroll'ni faqat shu holda bajarish kerak, aks holda
// yuqoriga scroll qilib o'qiyotgan foydalanuvchi siljitiladi.
func isNearBottom(el js.Value, threshold int) bool {
	scrollTop := el.Get("scrollTop").Int()
	clientH := el.Get("clientHeight").Int()
	scrollH := el.Get("scrollHeight").Int()
	return scrollH-scrollTop-clientH <= threshold
}

// GetCookie — JS cookie o'qish (key:value; key2:...). Go WASM da document.cookie.
func GetCookie(name string) string {
	c := document.Get("cookie").String()
	prefix := name + "="
	parts := splitSemicolons(c)
	for _, p := range parts {
		p = trimSpaceStr(p)
		if len(p) >= len(prefix) && p[:len(prefix)] == prefix {
			return p[len(prefix):]
		}
	}
	return ""
}

func splitSemicolons(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpaceStr(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// JS — global JS qiymatiga murojaat.
func JS(name string) js.Value {
	return js.Global().Get(name)
}

var _ = js.Value{}
