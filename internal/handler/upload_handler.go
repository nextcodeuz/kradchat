package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"kradchat/internal/middleware"
	"kradchat/internal/model"
	"kradchat/internal/service"
)

type UploadHandler struct {
	svc *service.Services
}

func NewUploadHandler(svc *service.Services) *UploadHandler {
	return &UploadHandler{svc: svc}
}

// Upload — chunked upload.
// Body: raw binary chunk. Parametrlar:
//   - filename (1-chunkda)
//   - size (1-chunkda, bayt)
//   - total (chunklar soni)
//   - index (0..total-1)
//   - upload_id (2+ chunkda)
//
// 1-chunk (upload_id yo'q) yangi sessiya ochadi va upload_id qaytaradi.
func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	uploadID := r.URL.Query().Get("upload_id")
	index, _ := strconv.Atoi(r.URL.Query().Get("index"))
	total, _ := strconv.Atoi(r.URL.Query().Get("total"))
	filename := r.URL.Query().Get("filename")
	size, _ := strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)

	if size < 0 || total < 0 || index < 0 {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid upload parameters")
		return
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024))
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "read_error", "failed to read body")
		return
	}

	var resp *model.UploadResponse
	userID := middleware.GetUserID(r)
	if uploadID == "" {
		// Birinchi chunk: filename majburiy; keyingi chunklar upload_id siz
		// har safar yangi sessiya ochmasligi uchun index>0 da upload_id shart.
		if filename == "" {
			middleware.WriteError(w, http.StatusBadRequest, "bad_request", "filename is required")
			return
		}
		if index > 0 {
			middleware.WriteError(w, http.StatusBadRequest, "bad_request", "upload_id is required")
			return
		}
		if total < 1 || total > 2048 {
			middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid total chunks")
			return
		}
		st, err := h.svc.Upload.Start(userID, filename, size, total)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		uploadID = st.ID
	}

	pct, resp, err := h.svc.Upload.Chunk(r.Context(), userID, uploadID, index, total, data)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if resp == nil {
		resp = &model.UploadResponse{}
	}
	resp.Progress = pct
	resp.UploadID = uploadID
	middleware.WriteJSON(w, http.StatusOK, resp)
}

// Status — upload progress (dumaloq indikator uchun).
func (h *UploadHandler) Status(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pct, err := h.svc.Upload.StatusFor(middleware.GetUserID(r), id)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"upload_id": id, "progress": pct})
}

// Cancel — yarim qolgan upload sessiyasini bekor qiladi (tmp tozalanadi).
// DELETE /api/upload/{id}
func (h *UploadHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "missing upload id")
		return
	}
	if err := h.svc.Upload.Cancel(middleware.GetUserID(r), id); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ListFiles — foydalanuvchining fayllari (Fayllarim ro'yxati).
// GET /api/files?limit=&offset=
func (h *UploadHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	files, err := h.svc.Upload.ListFiles(r.Context(), middleware.GetUserID(r), limit, offset)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if files == nil {
		files = []*model.FileRecord{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"files": files})
}

// DeleteFile — o'z faylini o'chiradi (DB yozuv + disk).
// DELETE /api/files/{id}
func (h *UploadHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "missing file id")
		return
	}
	if err := h.svc.Upload.DeleteFile(r.Context(), middleware.GetUserID(r), id); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// renameRequest — nom o'zgartirish body.
type renameRequest struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

// RenameFile — Fayllarim ekranidan nom o'zgartirish.
// PATCH /api/files/{id} {filename}
func (h *UploadHandler) RenameFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "missing file id")
		return
	}
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	rec, err := h.svc.Upload.RenameFile(r.Context(), middleware.GetUserID(r), id, req.Filename)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, rec)
}

// RenameFileByURL — chatdan nom o'zgartirish (fayl URL bo'yicha).
// PATCH /api/files/by-url {url, filename}
func (h *UploadHandler) RenameFileByURL(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.URL == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "missing url")
		return
	}
	rec, err := h.svc.Upload.RenameFileByURL(r.Context(), middleware.GetUserID(r), req.URL, req.Filename)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, rec)
}

// SaveFileToMy — chatdagi faylni shaxsiy nusxa ko'chirib o'z Fayllarimga qo'shadi.
// POST /api/files/save {url, filename}
func (h *UploadHandler) SaveFileToMy(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.URL == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "missing url")
		return
	}
	rec, err := h.svc.Upload.SaveFileToMy(r.Context(), middleware.GetUserID(r), req.URL, req.Filename)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, rec)
}
