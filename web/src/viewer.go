//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"
)

// Media viewer — fayllar xom static URL'da emas, ilova ichida ochiladi.
// < > — oldingi/keyingi media; video/audio o'z play kontrolleri bilan;
// Esc/yubka bosish — yopish.

// viewerMedias — joriy xonadagi media xabarlar (ochilish paytidagi snapshot).
var viewerMedias []*Msg
var viewerIdx int
var viewerKeysWired bool

// isViewerMedia — viewerda ko'rsatiladigan turlar (rasm/video/musiqa).
func isViewerMedia(m *Msg) bool {
	if m == nil || m.Type == "deleted" {
		return false
	}
	switch m.Type {
	case "image", "video":
		return m.FileURL != ""
	case "file":
		return isAudioURL(m.FileURL)
	}
	return false
}

// viewerSame — viewer ro'yxatida bosilgan xabarni topish (pointer → ID → cmid).
func viewerSame(a, b *Msg) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	if a.ID > 0 && a.ID == b.ID {
		return true
	}
	return a.ClientMsgID != "" && a.ClientMsgID == b.ClientMsgID
}

// openViewer — xabar mediasini viewer'da ochadi.
func openViewer(m *Msg) {
	viewerMedias = viewerMedias[:0]
	viewerIdx = -1
	for _, it := range store.Messages {
		if !isViewerMedia(it) {
			continue
		}
		viewerMedias = append(viewerMedias, it)
		if viewerSame(it, m) {
			viewerIdx = len(viewerMedias) - 1
		}
	}
	if viewerIdx < 0 || len(viewerMedias) == 0 {
		return
	}
	closeViewer()
	ov := El("div", "viewer-overlay")
	Attr(ov, "id", "viewer-overlay")
	box := El("div", "viewer-box glass")
	head := El("div", "viewer-head")
	count := El("div", "viewer-count")
	Attr(count, "id", "viewer-count")
	name := El("div", "viewer-name")
	Attr(name, "id", "viewer-name")
	closeBtn := El("button", "icon-btn")
	Attr(closeBtn, "title", "Yopish")
	SetText(closeBtn, "✕")
	On(closeBtn, "click", func(_ js.Value, _ []js.Value) { closeViewer() })
	Append(head, count)
	Append(head, name)
	Append(head, closeBtn)
	stage := El("div", "viewer-stage")
	Attr(stage, "id", "viewer-stage")
	foot := El("div", "viewer-foot")
	prev := El("button", "viewer-nav")
	Attr(prev, "id", "viewer-prev")
	Attr(prev, "title", "Oldingi")
	SetText(prev, "‹")
	On(prev, "click", func(_ js.Value, _ []js.Value) { viewerNav(-1) })
	next := El("button", "viewer-nav")
	Attr(next, "id", "viewer-next")
	Attr(next, "title", "Keyingi")
	SetText(next, "›")
	On(next, "click", func(_ js.Value, _ []js.Value) { viewerNav(1) })
	dl := El("a", "viewer-dl")
	Attr(dl, "id", "viewer-dl")
	SetText(dl, "⬇ Yuklab olish")
	Append(foot, prev)
	Append(foot, dl)
	Append(foot, next)
	Append(box, head)
	Append(box, stage)
	Append(box, foot)
	Append(ov, box)
	On(ov, "click", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		// Faqat orqa fonga bosilganda yopiladi (kontentga emas).
		if args[0].Get("target").Get("id").String() == "viewer-overlay" {
			closeViewer()
		}
	})
	Append(document.Get("body"), ov)
	wireViewerKeys()
	renderViewer()
}

// renderViewer — joriy media'ni chizadi.
func renderViewer() {
	if viewerIdx < 0 || viewerIdx >= len(viewerMedias) {
		return
	}
	m := viewerMedias[viewerIdx]
	SetText(ByID("viewer-count"), strconv.Itoa(viewerIdx+1)+" / "+strconv.Itoa(len(viewerMedias)))
	name := m.Content
	if name == "" {
		name = fileBaseName(m.FileURL)
	}
	SetText(ByID("viewer-name"), name)
	stage := ByID("viewer-stage")
	if stage.IsNull() || stage.IsUndefined() {
		return
	}
	HTML(stage, "")
	switch {
	case m.Type == "image":
		src := safeFileURL(m.FileURL)
		if prev := safeFileURL(m.PreviewURL); prev != "" {
			src = prev
		}
		if src != "" {
			img := El("img", "viewer-img")
			Attr(img, "src", src)
			Attr(img, "alt", name)
			Append(stage, img)
		}
	case m.Type == "video":
		if src := safeFileURL(m.FileURL); src != "" {
			vid := El("video", "viewer-video")
			Attr(vid, "src", src)
			Attr(vid, "controls", "true")
			Attr(vid, "preload", "metadata")
			vid.Set("autoplay", true)
			Append(stage, vid)
		}
	case m.Type == "file" && isAudioURL(m.FileURL):
		Append(stage, renderAudioPlayer(m))
	}
	dl := ByID("viewer-dl")
	if !dl.IsNull() && !dl.IsUndefined() {
		if href := safeFileURL(m.FileURL); href != "" {
			Attr(dl, "href", href)
			Attr(dl, "download", fileBaseName(m.FileURL))
			RemoveClass(dl, "hidden")
		} else {
			AddClass(dl, "hidden")
		}
	}
	toggleViewerNav()
}

// toggleViewerNav — chekkada tugmalarni o'chirish.
func toggleViewerNav() {
	prev := ByID("viewer-prev")
	next := ByID("viewer-next")
	if prev.IsNull() || prev.IsUndefined() || next.IsNull() || next.IsUndefined() {
		return
	}
	if viewerIdx <= 0 {
		AddClass(prev, "btn-disabled")
	} else {
		RemoveClass(prev, "btn-disabled")
	}
	if viewerIdx >= len(viewerMedias)-1 {
		AddClass(next, "btn-disabled")
	} else {
		RemoveClass(next, "btn-disabled")
	}
}

// viewerNav — oldingi/keyingi media (chegarada to'xtaydi).
func viewerNav(d int) {
	nidx := viewerIdx + d
	if nidx < 0 || nidx >= len(viewerMedias) {
		return
	}
	viewerIdx = nidx
	renderViewer()
}

// closeViewer — viewer'ni yopadi.
func closeViewer() {
	ov := ByID("viewer-overlay")
	if !ov.IsNull() && !ov.IsUndefined() {
		Remove(ov)
	}
	viewerMedias = nil
	viewerIdx = -1
}

// viewerOpen — viewer ochiqmi.
func viewerOpen() bool {
	ov := ByID("viewer-overlay")
	return !ov.IsNull() && !ov.IsUndefined()
}

// wireViewerKeys — Esc/←/→ (bir marta; input ichida o'chirilgan).
func wireViewerKeys() {
	if viewerKeysWired {
		return
	}
	viewerKeysWired = true
	f := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if !viewerOpen() || len(args) == 0 {
			return nil
		}
		ae := document.Get("activeElement")
		if !ae.IsNull() && !ae.IsUndefined() {
			tag := ae.Get("tagName").String()
			if tag == "INPUT" || tag == "TEXTAREA" {
				return nil
			}
			if ce := ae.Get("isContentEditable"); !ce.IsUndefined() && ce.Bool() {
				return nil
			}
		}
		switch args[0].Get("key").String() {
		case "Escape":
			closeViewer()
		case "ArrowLeft":
			viewerNav(-1)
		case "ArrowRight":
			viewerNav(1)
		}
		return nil
	})
	document.Call("addEventListener", "keydown", f)
	keptFuncs = append(keptFuncs, f)
}
