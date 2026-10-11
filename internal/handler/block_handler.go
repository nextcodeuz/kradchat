package handler

import (
	"net/http"

	"kradchat/internal/middleware"
	"kradchat/internal/service"
)

// BlockHandler — bloklash / blokdan olish endpointlari.
type BlockHandler struct {
	svc *service.Services
}

func NewBlockHandler(svc *service.Services) *BlockHandler {
	return &BlockHandler{svc: svc}
}

// Block — foydalanuvchini bloklaydi (POST /api/users/{id}/block).
// Bloklash ikki tomonlama: ikkala yo'nalishda ham DM xabarlari to'xtaydi,
// broadcast'larda bir-birining xabarlari ko'rinmaydi.
func (h *BlockHandler) Block(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "user id kerak")
		return
	}
	viewerID := middleware.GetUserID(r)
	if err := h.svc.Blocks.Block(r.Context(), viewerID, targetID); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"blocked": true})
}

// Unblock — foydalanuvchini blokdan olib tashlaydi (DELETE /api/users/{id}/block).
func (h *BlockHandler) Unblock(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "user id kerak")
		return
	}
	viewerID := middleware.GetUserID(r)
	if err := h.svc.Blocks.Unblock(r.Context(), viewerID, targetID); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"blocked": false})
}
