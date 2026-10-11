package handler

import (
	"encoding/json"
	"net/http"

	"kradchat/internal/middleware"
	"kradchat/internal/model"
	"kradchat/internal/service"
)

type BackgroundHandler struct {
	svc *service.Services
}

func NewBackgroundHandler(svc *service.Services) *BackgroundHandler {
	return &BackgroundHandler{svc: svc}
}

func (h *BackgroundHandler) List(w http.ResponseWriter, r *http.Request) {
	bgs, err := h.svc.Background.List(r.Context())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	var out []map[string]any
	for _, bg := range bgs {
		out = append(out, map[string]any{
			"id":       bg.ID,
			"name":     bg.Name,
			"category": bg.Category,
			"url":      "/static/backgrounds/" + bg.FilePath,
		})
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"backgrounds": out})
}

func (h *BackgroundHandler) Set(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackgroundID string `json:"background_id"`
		Room         string `json:"room"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if req.Room == "" {
		req.Room = model.RoomGlobal
	}
	if err := h.svc.Background.SetBackground(r.Context(), middleware.GetUserID(r), req.BackgroundID, req.Room); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
