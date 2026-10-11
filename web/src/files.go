//go:build js && wasm

package main

import (
	"syscall/js"
	"time"
)

// Fayllarim — foydalanuvchining yuklangan fayllari ro'yxati va o'chirish.

type FileItem struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Mime       string `json:"mime"`
	Size       int64  `json:"size"`
	URL        string `json:"url"`
	PreviewURL string `json:"preview_url"`
	Kind       string `json:"kind"`
	CreatedAt  string `json:"created_at"`
}

func renderFiles(app js.Value) {
	HTML(app, filesLayout())
	fillMeCard()
	SetText(ByID("files-msg"), "Yuklanmoqda...")
	go func() {
		var out struct {
			Files []*FileItem `json:"files"`
		}
		if err := fetchJSONInto("GET", "/api/files?limit=50", nil, &out); err != nil {
			SetText(ByID("files-msg"), "❌ "+err.Error())
			return
		}
		SetText(ByID("files-msg"), "")
		list := ByID("files-list")
		HTML(list, "")
		for _, f := range out.Files {
			row := El("div", "device-row glass")
			icon := El("div", "device-icon")
			SetText(icon, fileKindIcon(f.Mime))
			meta := El("div", "device-meta")
			nm := El("div", "device-name")
			name := f.Filename
			if name == "" {
				name = fileBaseName(f.URL)
			}
			SetText(nm, name)
			info := El("div", "device-info")
			SetText(info, humanSize(f.Size)+" · "+f.Mime+" · "+shortFileTime(f.CreatedAt))
			Append(meta, nm)
			Append(meta, info)
			// Ochish havolasi (xavfsiz URL).
			if dl := safeFileURL(f.URL); dl != "" {
				open := El("a", "btn-small")
				open.Call("setAttribute", "href", dl)
				open.Call("setAttribute", "target", "_blank")
				open.Call("setAttribute", "rel", "noopener noreferrer")
				SetText(open, "Ochish")
				Append(row, open)
			}
			ren := El("button", "btn-small")
			SetText(ren, "✏️")
			Attr(ren, "title", "Nomini o'zgartirish")
			fid := f.ID
			On(ren, "click", func(_ js.Value, _ []js.Value) {
				wireRenameInline(nm, fileDisplayName(f), func(newName string) {
					go func() {
						var out FileItem
						if err := patchFileByID(fid, newName, &out); err != nil {
							showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Nom o'zgarmadi"})
							SetText(nm, fileDisplayName(f))
							return
						}
						f.Filename = out.Filename
						SetText(nm, fileDisplayName(f))
						showToast(&Notif{Type: "ok", Title: "Saqladim", Body: "Fayl nomi yangilandi"})
					}()
				})
			})
			Append(row, ren)
			del := El("button", "btn-danger btn-small")
			SetText(del, "O'chirish")
			SetData(del, "fileid", f.ID)
			On(del, "click", func(_ js.Value, _ []js.Value) {
				confirmDeleteFile(del, f.ID)
			})
			Append(row, icon)
			Append(row, meta)
			Append(list, row)
		}
		if len(out.Files) == 0 {
			SetText(list, "Hali fayl yuklamagansiz")
		}
	}()
	wireNav(app)
}

// confirmDeleteFile — ikki bosqichli tasdiq: 1-bosishda "Tasdiqla?", 3s ichida
// 2-bosishda o'chadi (tasodifiy o'chirish oldi).
func confirmDeleteFile(btn js.Value, id string) {
	if GetData(btn, "armed") == "1" {
		go func() {
			if _, err := doFetch("DELETE", "/api/files/"+id, nil, true, ""); err != nil {
				showToast(&Notif{Type: "error", Title: "Xatolik", Body: "O'chirib bo'lmadi"})
				return
			}
			row := btn.Call("closest", ".device-row")
			if !row.IsNull() && !row.IsUndefined() {
				Remove(row)
			}
			showToast(&Notif{Type: "ok", Title: "O'chirildi", Body: "Fayl o'chirildi"})
		}()
		return
	}
	SetData(btn, "armed", "1")
	SetText(btn, "Tasdiqla?")
	var fn js.Func
	fn = js.FuncOf(func(js.Value, []js.Value) any {
		defer fn.Release()
		if btn.IsNull() || btn.IsUndefined() {
			return nil
		}
		SetData(btn, "armed", "")
		SetText(btn, "O'chirish")
		return nil
	})
	js.Global().Call("setTimeout", fn, 3000)
}

// fileDisplayName — ko'rinadigan fayl nomi (bo'sh bo'lsa URL oxiri).
func fileDisplayName(f *FileItem) string {
	if f.Filename != "" {
		return f.Filename
	}
	return fileBaseName(f.URL)
}

// patchFileByID — PATCH /api/files/{id} {filename}.
func patchFileByID(id, filename string, out *FileItem) error {
	resp, err := fetchJSON("PATCH", "/api/files/"+id, map[string]any{
		"filename": filename,
	}, true)
	if err != nil {
		return err
	}
	return unmarshalResponse(resp, out)
}

// fileKindIcon — mime bo'yicha emoji belgi.
func fileKindIcon(mime string) string {
	switch {
	case len(mime) >= 6 && mime[:6] == "image/":
		return "🖼"
	case len(mime) >= 6 && mime[:6] == "video/":
		return "🎬"
	case len(mime) >= 6 && mime[:6] == "audio/":
		return "🎵"
	case mime == "application/pdf":
		return "📕"
	case mime == "application/zip":
		return "📦"
	default:
		return "📄"
	}
}

// shortFileTime — "2026-09-23T..." ISO vaqtni qisqa ko'rinishga o'tkazadi.
func shortFileTime(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format("02.01 15:04")
	}
	if len(s) >= 16 {
		return s[:16]
	}
	return s
}

func filesLayout() string {
	return `<div class="layout">
  ` + mobileBar() + `
  <aside class="sidebar glass">
    <div class="brand">KRAD<span>CHAT</span></div>
    <div class="me-card">
      <div class="me-avatar" id="me-avatar"></div>
      <div class="me-meta">
        <div class="me-name" id="me-name"></div>
        <div class="me-sub" id="me-sub"></div>
      </div>
      <button class="icon-btn" id="btn-logout" title="Chiqish" aria-label="Chiqish"><svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor" aria-hidden="true"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg></button>
    </div>
    <nav class="side-nav">
      <button class="nav-item" id="nav-chat">💬 Umumiy chat</button>
      <button class="nav-item" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item active" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
  </aside>
  <main class="screen-main">
    <header class="screen-header">Fayllarim</header>
    <div class="screen-body">
      <section class="panel glass">
        <div class="hint" id="files-msg"></div>
        <div id="files-list"></div>
      </section>
    </div>
  </main>
</div>`
}
