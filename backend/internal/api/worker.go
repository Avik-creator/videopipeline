package api

import (
	"log"

	"videopipe/internal/db"
	"videopipe/internal/storage"
	"videopipe/internal/transcode"
)

// StartWorkers launches n goroutines that process transcode jobs from the jobs channel.
func StartWorkers(n int, jobs <-chan string, database *db.DB, store *storage.Store) {
	for i := range n {
		go func(id int) {
			log.Printf("[worker %d] started", id)
			for videoID := range jobs {
				log.Printf("[worker %d] processing %s", id, videoID)
				processJob(videoID, database, store)
			}
		}(i)
	}
}

func processJob(videoID string, database *db.DB, store *storage.Store) {
	// Mark as processing
	if err := database.SetStatus(videoID, "processing", ""); err != nil {
		log.Printf("[transcode] %s: set processing failed: %v", videoID, err)
		return
	}

	inputPath := store.RawPath(videoID)
	hlsDir, err := store.HLSVideoDir(videoID)
	if err != nil {
		database.SetStatus(videoID, "failed", "could not create HLS dir: "+err.Error())
		return
	}
	thumbPath := store.ThumbPath(videoID)

	resolutions := transcode.DefaultResolutions()
	result, err := transcode.Transcode(inputPath, hlsDir, thumbPath, resolutions)
	if err != nil {
		log.Printf("[transcode] %s: failed: %v", videoID, err)
		database.SetStatus(videoID, "failed", err.Error())
		return
	}

	if err := database.SetReady(videoID, result.MasterPlaylist, result.ThumbPath,
		result.DurationS, result.Resolution); err != nil {
		log.Printf("[transcode] %s: set ready failed: %v", videoID, err)
		return
	}

	// Delete raw file to save disk space (comment out to keep originals)
	if err := store.DeleteRaw(videoID); err != nil {
		log.Printf("[transcode] %s: delete raw: %v", videoID, err)
	}

	log.Printf("[transcode] %s: done (%ds, %s)", videoID, result.DurationS, result.Resolution)
}
