//go:build js && wasm

package main

import (
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

// Fayl upload — chunked, dumaloq progress indikator.

const uploadChunkSize = 1024 * 1024    // 1MB
const uploadMaxSize = 50 * 1024 * 1024 // server MAX_FILE_SIZE bilan mos

// uploadAbort — joriy upload'ni bekor qilish bayrog'i (chunklar orasida tekshiriladi).
var uploadAbort = false
var uploadSessionID = ""

// pendingFile — 100% yuklangan, lekin foydalanuvchi "Yuborish" tugmasini
// bosmaguncha yuborilmaydigan fayl. Avtomatik yuborish YO'Q — yuborish faqat
// foydalanuvchi nazoratida (oldin 100% da o'zi ketib qolardi).
type pendingFileState struct {
	active               bool
	url, preview         string
	name, mime, uploadID string
	size                 int
}

var pendingFile = pendingFileState{}

// allowedUploadExt — client-side tezkor tekshiruv (server magic bytes hal qiluvchi).
var allowedUploadExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".mp4": true, ".webm": true, ".pdf": true, ".zip": true, ".txt": true,
	".csv": true, ".mp3": true, ".wav": true, ".ogg": true,
}

func uploadFile(file js.Value) {
	name := file.Get("name").String()
	size := file.Get("size").Int()
	if size <= 0 {
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Bo'sh fayl"})
		return
	}
	// Client-side pre-check — serverga borib qaytmasdan tez xato.
	if size > uploadMaxSize {
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Fayl juda katta (50MB chegara)"})
		return
	}
	if !allowedUploadExt[uploadExt(name)] {
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Fayl turi ruxsat etilmagan"})
		return
	}
	// Progress panel
	panel := ByID("upload-progress")
	RemoveClass(panel, "hidden")
	HTML(panel, `<div class="up-circle">
    <svg class="up-svg" viewBox="0 0 36 36">
      <path class="up-bg" d="M18 2.08a15.92 15.92 0 1 0 0.001 0z"/>
      <path class="up-fg" id="up-arc" d="M18 2.08a15.92 15.92 0 1 0 0.001 0z"/>
    </svg>
    <div class="up-label" id="up-label">0%</div>
  </div>
  <div class="up-meta">
    <div class="up-name" id="up-name"></div>
    <div class="up-sub" id="up-sub"></div>
  </div>
  <button class="icon-btn" id="up-cancel" title="Bekor qilish">✕</button>`)
	SetText(ByID("up-name"), name)
	SetText(ByID("up-sub"), "Tayyorlanmoqda...")
	uploadAbort = false
	uploadSessionID = ""
	pendingFile = pendingFileState{} // eski kutayotgan fayl (bo'lsa) bekor hisoblanadi
	On(ByID("up-cancel"), "click", func(_ js.Value, _ []js.Value) {
		uploadAbort = true
		if id := uploadSessionID; id != "" {
			go func() {
				_, _ = doFetch("DELETE", "/api/upload/"+id, nil, true, "")
			}()
		}
	})

	// MUHIM: upload butun oqimi alohida goroutinada ishlaydi.
	// FileReader onload callback'i faqat kanalga yuboradi (bloklanmaydi).
	// JS callback ichida awaitJS (bloklanuvchi fetch) chaqirilsa
	// butun tab muzlab qoladi (Go WASM deadlock).
	go func() {
		dataCh := make(chan []byte, 1)
		readBytes(file, dataCh)
		data := <-dataCh
		if data == nil {
			uploadDone(false, "Faylni o'qib bo'lmadi")
			return
		}
		total := (len(data) + uploadChunkSize - 1) / uploadChunkSize
		if total == 0 {
			total = 1
		}
		uploadID := ""
		for i := 0; i < total; i++ {
			if uploadAbort {
				uploadDone(false, "Bekor qilindi")
				return
			}
			start := i * uploadChunkSize
			end := start + uploadChunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := data[start:end]
			// Bitta retry: tarmoq miltillaganda upload o'lmaydi.
			pct, errMsg := uploadChunk(uploadID, name, size, total, i, chunk)
			if pct < 0 {
				time.Sleep(400 * time.Millisecond)
				if uploadAbort {
					uploadDone(false, "Bekor qilindi")
					return
				}
				pct, errMsg = uploadChunk(uploadID, name, size, total, i, chunk)
			}
			if pct < 0 {
				uploadDone(false, errMsg)
				return
			}
			if uploadID == "" {
				uploadID = uploadResultID
				uploadSessionID = uploadID
			}
			setProgress(pct)
		}
		if uploadAbort {
			uploadDone(false, "Bekor qilindi")
			return
		}
		// 100% — lekin AVTOMATIK yuborilmaydi. Fayl kutish holatiga o'tadi:
		// foydalanuvchi "Yuborish" bossa ketadi, "Bekor" bossa ketmaydi
		// (serverdagi yetim fayl 24 soatda GC tozalaydi).
		pendingFile = pendingFileState{
			active:   true,
			url:      uploadResultURL,
			preview:  uploadResultPreview,
			name:     name,
			mime:     file.Get("type").String(),
			uploadID: uploadResultID,
			size:     size,
		}
		showPendingFile()
	}()
}

var uploadResultID string
var uploadResultURL string
var uploadResultPreview string

func setProgress(pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	label := ByID("up-label")
	if !label.IsNull() {
		SetText(label, strconv.Itoa(pct)+"%")
	}
	arc := ByID("up-arc")
	if !arc.IsNull() {
		arc.Set("strokeDasharray", strconv.Itoa(pct*283/100)+" 283")
	}
	sub := ByID("up-sub")
	if !sub.IsNull() {
		SetText(sub, strconv.Itoa(pct)+"% yuklandi")
	}
}

func uploadDone(ok bool, msg string) {
	panel := ByID("upload-progress")
	if panel.IsNull() {
		return
	}
	if ok {
		var fn js.Func
		fn = js.FuncOf(func(js.Value, []js.Value) any {
			defer fn.Release()
			AddClass(panel, "hidden")
			return nil
		})
		js.Global().Call("setTimeout", fn, 1500)
	} else {
		SetText(ByID("up-sub"), "❌ "+msg)
	}
}

// showPendingFile — yuklangan faylni "yuborishga tayyor" ko'rinishida chiqaradi.
// Yuborish faqat #up-send bosilganda; #up-discard — yubormasdan yopish.
func showPendingFile() {
	panel := ByID("upload-progress")
	if panel.IsNull() || !pendingFile.active {
		return
	}
	HTML(panel, `<div class="up-file" id="up-ready-icon"></div>
  <div class="up-meta">
    <div class="up-name" id="up-ready-name"></div>
    <div class="up-sub" id="up-ready-sub"></div>
  </div>
  <button class="btn-send" id="up-send" title="Yuborish">➤</button>
  <button class="icon-btn" id="up-discard" title="Yubormasdan yopish">✕</button>`)
	SetText(ByID("up-ready-icon"), pendingFileIcon(pendingFile.mime))
	SetText(ByID("up-ready-name"), pendingFile.name)
	SetText(ByID("up-ready-sub"), "Tayyor ✓ — yuborish uchun ➤ bosing")
	On(ByID("up-send"), "click", func(_ js.Value, _ []js.Value) {
		f := pendingFile
		pendingFile = pendingFileState{}
		if !f.active || f.url == "" {
			return
		}
		sendFileMessage(f.url, f.preview, f.size, f.name, f.mime, f.uploadID)
		AddClass(ByID("upload-progress"), "hidden")
	})
	On(ByID("up-discard"), "click", func(_ js.Value, _ []js.Value) {
		pendingFile = pendingFileState{}
		AddClass(ByID("upload-progress"), "hidden")
		showToast(&Notif{Type: "ok", Title: "Bekor qilindi", Body: "Fayl yuborilmadi"})
	})
}

// pendingFileIcon — mime bo'yicha belgi.
func pendingFileIcon(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "🖼"
	case strings.HasPrefix(mime, "audio/"):
		return "🎵"
	case strings.HasPrefix(mime, "video/"):
		return "🎬"
	default:
		return "📄"
	}
}

// uploadExt — fayl kengaytmasi (kichik harfda, nuqta bilan).
func uploadExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return ""
	}
	return strings.ToLower(name[idx:])
}

// sendFileMessage — yuklangan faylni xabar sifatida yuboradi.
// msg_type fayl mime'iga qarab: image/video/file (server tarafda validatsiya).
func sendFileMessage(url, preview string, size int, name, mime, uploadID string) {
	msgType := "file"
	switch {
	case strings.HasPrefix(mime, "image/"):
		msgType = "image"
	case strings.HasPrefix(mime, "video/"):
		msgType = "video"
	}
	cmid := newClientMsgID()
	sendWS(map[string]any{
		"type":          "msg",
		"room":          store.CurRoom,
		"content":       name,
		"client_msg_id": cmid,
		"msg_type":      msgType,
		"file_url":      url,
		"preview_url":   preview,
		"file_size":     size,
		"upload_id":     uploadID,
	})
}

// readBytes — File'ni []byte ga o'qiydi (FileReader).
// Natija ch kanaliga yuboriladi; xatoda nil yuboriladi.
// js.Func qiymatlari Release qilinmaguncha alive saqlanadi.
func readBytes(file js.Value, ch chan<- []byte) {
	reader := js.Global().Get("FileReader").New()
	var okFn, errFn js.Func
	okFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		buf := reader.Get("result")
		u8 := js.Global().Get("Uint8Array").New(buf)
		data := make([]byte, u8.Length())
		js.CopyBytesToGo(data, u8)
		okFn.Release()
		errFn.Release()
		ch <- data
		return nil
	})
	errFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		okFn.Release()
		errFn.Release()
		ch <- nil
		return nil
	})
	reader.Set("onload", okFn)
	reader.Set("onerror", errFn)
	reader.Call("readAsArrayBuffer", file)
}

// uploadPickedFiles — tanlangan/tashlangan fayllar ro'yxatini yuklaydi (max 3).
func uploadPickedFiles(files js.Value) {
	if files.IsNull() || files.IsUndefined() {
		return
	}
	n := files.Length()
	if n > 3 {
		n = 3
		showToast(&Notif{Type: "error", Title: "Chegara", Body: "Bir vaqtda max 3 fayl"})
	}
	for i := 0; i < n; i++ {
		uploadFile(files.Index(i))
	}
}

// wireFileDrop — drop + paste orqali fayl yuklash (composer atrofida).
func wireFileDrop(input js.Value) {
	if input.IsNull() || input.IsUndefined() {
		return
	}
	dropZone := ByID("messages")
	if dropZone.IsNull() || dropZone.IsUndefined() {
		dropZone = input
	}
	On(dropZone, "dragover", func(_ js.Value, args []js.Value) {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
	})
	On(dropZone, "drop", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		ev := args[0]
		ev.Call("preventDefault")
		dt := ev.Get("dataTransfer")
		if dt.IsNull() || dt.IsUndefined() {
			return
		}
		uploadPickedFiles(dt.Get("files"))
	})
	On(input, "paste", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		ev := args[0]
		cd := ev.Get("clipboardData")
		if cd.IsNull() || cd.IsUndefined() {
			return
		}
		files := cd.Get("files")
		if !files.IsNull() && !files.IsUndefined() && files.Length() > 0 {
			ev.Call("preventDefault")
			uploadPickedFiles(files)
		}
	})
}

// uploadChunk — bitta chunkni yuboradi. Qaytaradi: progress (0-100) yoki
// -1 xatolik (ikkinchi qiymat — ko'rsatiladigan xato matni).
func uploadChunk(uploadID, name string, size, total, index int, chunk []byte) (int, string) {
	params := "index=" + strconv.Itoa(index) + "&total=" + strconv.Itoa(total) +
		"&size=" + strconv.Itoa(size)
	if index == 0 {
		params += "&filename=" + js.Global().Get("encodeURIComponent").Invoke(name).String()
	} else {
		params += "&upload_id=" + uploadID
	}
	opts := js.Global().Get("Object").New()
	opts.Set("method", "POST")
	headers := js.Global().Get("Object").New()
	headers.Set("Authorization", "Bearer "+store.AccessToken)
	opts.Set("headers", headers)
	u8 := js.Global().Get("Uint8Array").New(len(chunk))
	js.CopyBytesToJS(u8, chunk)
	opts.Set("body", u8)

	respV, err := awaitJS(js.Global().Call("fetch", "/api/upload?"+params, opts))
	if err != nil {
		return -1, "Tarmoq xatosi"
	}
	if !respV.Get("ok").Bool() {
		// Server xato tafsilotini ko'rsatishga harakat qilamiz.
		detail := "Upload xatosi"
		if body, e := awaitJS(respV.Call("json")); e == nil {
			if c := body.Get("code").String(); c != "" {
				switch c {
				case "invalid_file":
					detail = "Fayl turi ruxsat etilmagan"
				case "file_too_large":
					detail = "Fayl juda katta (50MB chegara)"
				case "chunk_error":
					detail = "Chunk xatosi, qayta urining"
				case "not_own_file", "forbidden":
					detail = "Bu fayl sizga tegishli emas"
				default:
					detail = "Upload xatosi (" + c + ")"
				}
			}
		}
		return -1, detail
	}
	data, err := awaitJS(respV.Call("json"))
	if err != nil {
		return -1, "Javobni o'qib bo'lmadi"
	}
	uploadResultID = data.Get("upload_id").String()
	uploadResultURL = data.Get("url").String()
	uploadResultPreview = data.Get("preview_url").String()
	pct := data.Get("progress").Int()
	return pct, ""
}

func nowUnixNano() int64 {
	return time.Now().UnixNano()
}
