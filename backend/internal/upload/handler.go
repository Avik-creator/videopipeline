package upload

import (
	"encoding/json"
	"fmt"
	"net/http"

	"videopipe/internal/db"
	"videopipe/internal/storage"

	"github.com/google/uuid"
)

const maxUploadSize = 10 << 30 // 10 GB

type Handler struct {
	store *storage.Store
	db    *db.DB
	jobs  chan<- string
}

func NewHandler(store *storage.Store, database *db.DB, jobs chan<- string) *Handler {
	return &Handler{store, database, jobs}
}

// InitUpload creates a new video record and returns its ID.
// POST /api/videos/init
func (h *Handler) InitUpload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title    string `json:"title"`
		FileSize int64  `json:"file_size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	videoID := uuid.New().String()
	v := &db.Video{ID: videoID, Title: body.Title, FileSize: body.FileSize}
	if err := h.db.CreateVideo(v); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"video_id":     videoID,
		"upload_url":   fmt.Sprintf("/api/videos/%s/upload", videoID),
		"chunk_url":    fmt.Sprintf("/api/videos/%s/chunk", videoID),
		"resume_bytes": h.store.RawSize(videoID),
	})
}

// Upload handles a simple single-request upload.
// PUT /api/videos/{id}/upload
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("id")
	v, err := h.db.Get(videoID)
	if err != nil || v == nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	written, err := h.store.SaveUpload(videoID, r.Body)
	if err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.jobs <- videoID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"video_id": videoID, "bytes": written, "status": "queued"})
}

// UploadChunk handles one chunk of a resumable upload.
// PUT /api/videos/{id}/chunk  (Content-Range: bytes start-end/total)
func (h *Handler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("id")
	v, err := h.db.Get(videoID)
	if err != nil || v == nil {
		http.Error(w, "video not found", http.StatusNotFound)
		return
	}

	cr := r.Header.Get("Content-Range")
	if cr == "" {
		http.Error(w, "Content-Range required", http.StatusBadRequest)
		return
	}
	var start, end, total int64
	fmt.Sscanf(cr, "bytes %d-%d/%d", &start, &end, &total)

	written, err := h.store.SaveChunk(videoID, start, r.Body)
	if err != nil {
		http.Error(w, "chunk write error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	currentSize := h.store.RawSize(videoID)
	complete := currentSize >= total
	if complete {
		h.jobs <- videoID
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", currentSize-1))
	json.NewEncoder(w).Encode(map[string]any{
		"video_id":       videoID,
		"bytes_written":  written,
		"total_received": currentSize,
		"complete":       complete,
	})
}

// ResumeStatus returns how many bytes we already have.
// GET /api/videos/{id}/resume
func (h *Handler) ResumeStatus(w http.ResponseWriter, r *http.Request) {
	videoID := r.PathValue("id")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"video_id":     videoID,
		"resume_bytes": h.store.RawSize(videoID),
	})
}
