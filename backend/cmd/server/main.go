package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"videopipe/internal/api"
	"videopipe/internal/db"
	"videopipe/internal/storage"
	"videopipe/internal/upload"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataDir := flag.String("data", "./videos", "Base directory for video storage")
	dbPath := flag.String("db", "./videopipe.db", "SQLite database path")
	workers := flag.Int("workers", runtime.NumCPU(), "Number of transcode workers")
	flag.Parse()

	// Ensure data directories exist
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("cannot create data dir: %v", err)
	}

	// Storage
	store, err := storage.New(*dataDir)
	if err != nil {
		log.Fatalf("storage init: %v", err)
	}

	// Database
	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()

	// Job queue: buffered so uploads don't block
	jobs := make(chan string, 256)

	// Start transcode workers
	api.StartWorkers(*workers, jobs, database, store)

	// Re-queue any pending jobs from a previous run
	if pending, err := database.PendingJobs(); err == nil {
		for _, v := range pending {
			log.Printf("re-queuing pending job: %s (%s)", v.ID, v.Title)
			jobs <- v.ID
		}
	}

	// Handlers
	uploadHandler := upload.NewHandler(store, database, jobs)
	apiHandler := api.NewHandler(database, store)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Content-Range", "Range"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// ── API routes ────────────────────────────────────────────────────────────
	r.Route("/api/videos", func(r chi.Router) {
		r.Get("/", apiHandler.ListVideos)
		r.Post("/init", uploadHandler.InitUpload)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", apiHandler.GetVideo)
			r.Get("/status/stream", apiHandler.StatusSSE)
			r.Get("/resume", uploadHandler.ResumeStatus)
			r.Put("/upload", uploadHandler.Upload)
			r.Put("/chunk", uploadHandler.UploadChunk)
		})
	})

	// ── Static file serving ──────────────────────────────────────────────────
	// Serve HLS segments: /hls/{videoID}/...
	hlsFS := http.FileServer(http.Dir(store.HLSDir))
	r.Get("/hls/*", func(w http.ResponseWriter, req *http.Request) {
		// Add CORS + cache headers for HLS segments
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if filepath.Ext(req.URL.Path) == ".ts" {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=5")
		}
		http.StripPrefix("/hls/", hlsFS).ServeHTTP(w, req)
	})

	// Serve thumbnails: /thumbs/{videoID}.jpg
	thumbFS := http.FileServer(http.Dir(store.ThumbDir))
	r.Get("/thumbs/*", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.StripPrefix("/thumbs/", thumbFS).ServeHTTP(w, req)
	})

	// ── Frontend ─────────────────────────────────────────────────────────────
	// Serve the embedded frontend from ./frontend/
	frontendDir := "../frontend"
	frontendFS := http.FileServer(http.Dir(frontendDir))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		// SPA fallback: if file not found, serve index.html
		path := filepath.Join(frontendDir, filepath.Clean(req.URL.Path))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, req, filepath.Join(frontendDir, "index.html"))
			return
		}
		frontendFS.ServeHTTP(w, req)
	})

	log.Printf("▶ videopipe listening on %s  (workers: %d, data: %s)",
		*addr, *workers, *dataDir)
	log.Fatal(http.ListenAndServe(*addr, r))
}
