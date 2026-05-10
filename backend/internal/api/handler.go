package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"videopipe/internal/db"
	"videopipe/internal/storage"
)

type Handler struct {
	db    *db.DB
	store *storage.Store
}

func NewHandler(database *db.DB, store *storage.Store) *Handler {
	return &Handler{database, store}
}

// GetVideo returns video metadata.
func (h *Handler) GetVideo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, err := h.db.Get(id)
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}
	if v == nil {
		http.Error(w, "not found", 404)
		return
	}
	if v.Status == "ready" {
		h.db.IncrViews(id)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(enrichURLs(v))
}

// ListVideos returns paginated video list.
func (h *Handler) ListVideos(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	videos, err := h.db.List(limit, offset)
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}
	enriched := make([]any, len(videos))
	for i, v := range videos {
		enriched[i] = enrichURLs(v)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"videos": enriched,
		"limit":  limit,
		"offset": offset,
	})
}

// StatusSSE streams processing status updates via Server-Sent Events.
func (h *Handler) StatusSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	if h.sendSSEEvent(w, flusher, id) {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if h.sendSSEEvent(w, flusher, id) {
				return
			}
		}
	}
}

func (h *Handler) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, id string) bool {
	v, err := h.db.Get(id)
	if err != nil || v == nil {
		return true
	}
	data, _ := json.Marshal(enrichURLs(v))
	w.Write([]byte("data: "))
	w.Write(data)
	w.Write([]byte("\n\n"))
	flusher.Flush()
	return v.Status == "ready" || v.Status == "failed"
}

func enrichURLs(v *db.Video) map[string]any {
	out := map[string]any{
		"id":         v.ID,
		"title":      v.Title,
		"status":     v.Status,
		"views":      v.Views,
		"duration_s": v.DurationS,
		"file_size":  v.FileSize,
		"resolution": v.Resolution,
		"created_at": v.CreatedAt,
	}
	if v.HLSPath != "" {
		videoDir := filepath.Base(filepath.Dir(v.HLSPath))
		out["hls_url"] = "/hls/" + videoDir + "/master.m3u8"
	}
	if v.ThumbPath != "" {
		out["thumbnail"] = "/thumbs/" + filepath.Base(v.ThumbPath)
	}
	if v.ErrorMsg != "" {
		out["error"] = v.ErrorMsg
	}
	return out
}
