package transcode

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Resolution defines a quality variant.
type Resolution struct {
	Label   string // "360p", "720p", "1080p"
	Width   int
	Height  int
	Bitrate string // video bitrate e.g. "800k"
	Audio   string // audio bitrate e.g. "96k"
}

// DefaultResolutions returns the standard ladder.
func DefaultResolutions() []Resolution {
	return []Resolution{
		{"360p", 640, 360, "800k", "96k"},
		{"720p", 1280, 720, "2800k", "128k"},
		{"1080p", 1920, 1080, "5000k", "192k"},
	}
}

// Result holds post-transcode metadata.
type Result struct {
	MasterPlaylist string // relative path to master.m3u8
	ThumbPath      string
	DurationS      int
	Resolution     string // highest variant produced
}

// Transcode converts inputPath into HLS variants under hlsDir, and writes a
// thumbnail to thumbPath. It runs ffmpeg for each quality in parallel.
func Transcode(inputPath, hlsDir, thumbPath string, resolutions []Resolution) (*Result, error) {
	// Probe duration
	dur, err := probeDuration(inputPath)
	if err != nil {
		log.Printf("warn: could not probe duration: %v", err)
	}

	// Generate thumbnail at ~5s mark (or 1s if short)
	seek := "00:00:05"
	if dur > 0 && dur < 6 {
		seek = "00:00:01"
	}
	if err := generateThumb(inputPath, thumbPath, seek); err != nil {
		log.Printf("warn: thumbnail failed: %v", err)
	}

	type variantResult struct {
		res Resolution
		err error
	}
	ch := make(chan variantResult, len(resolutions))

	// Probe actual video width to skip upscaling
	actualW, _ := probeWidth(inputPath)

	var produced []Resolution
	for _, r := range resolutions {
		if actualW > 0 && actualW < r.Width {
			log.Printf("skip %s: source width %d < %d", r.Label, actualW, r.Width)
			continue
		}
		produced = append(produced, r)
		go func(r Resolution) {
			err := transcodeVariant(inputPath, hlsDir, r)
			ch <- variantResult{r, err}
		}(r)
	}

	// Collect results
	for range produced {
		vr := <-ch
		if vr.err != nil {
			return nil, fmt.Errorf("transcode %s: %w", vr.res.Label, vr.err)
		}
	}

	if len(produced) == 0 {
		// fallback: transcode at original resolution
		produced = append(produced, Resolution{"original", 0, 0, "2000k", "128k"})
		if err := transcodeOriginal(inputPath, hlsDir); err != nil {
			return nil, err
		}
	}

	// Write master playlist
	masterPath := filepath.Join(hlsDir, "master.m3u8")
	if err := writeMaster(masterPath, produced); err != nil {
		return nil, err
	}

	top := produced[len(produced)-1].Label
	return &Result{
		MasterPlaylist: masterPath,
		ThumbPath:      thumbPath,
		DurationS:      dur,
		Resolution:     top,
	}, nil
}

func transcodeVariant(input, hlsDir string, r Resolution) error {
	outDir := filepath.Join(hlsDir, r.Label)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	playlist := filepath.Join(outDir, "index.m3u8")
	segPattern := filepath.Join(outDir, "seg%03d.ts")

	args := []string{
		"-i", input,
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", r.Width, r.Height, r.Width, r.Height),
		"-c:v", "libx264",
		"-preset", "fast",
		"-b:v", r.Bitrate,
		"-maxrate", r.Bitrate,
		"-bufsize", doubleRate(r.Bitrate),
		"-c:a", "aac",
		"-b:a", r.Audio,
		"-ac", "2",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segPattern,
		"-f", "hls",
		playlist,
	}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func transcodeOriginal(input, hlsDir string) error {
	outDir := filepath.Join(hlsDir, "original")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	playlist := filepath.Join(outDir, "index.m3u8")
	segPattern := filepath.Join(outDir, "seg%03d.ts")

	cmd := exec.Command("ffmpeg",
		"-i", input,
		"-c:v", "libx264", "-preset", "fast", "-crf", "23",
		"-c:a", "aac", "-ac", "2",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", segPattern,
		"-f", "hls", playlist,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func generateThumb(input, out, seek string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	cmd := exec.Command("ffmpeg",
		"-ss", seek,
		"-i", input,
		"-vframes", "1",
		"-vf", "scale=1280:720:force_original_aspect_ratio=decrease",
		"-q:v", "2",
		"-y", out,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeMaster(path string, variants []Resolution) error {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n\n")
	bandwidths := map[string]int{
		"360p": 900000, "720p": 3000000, "1080p": 5200000, "original": 2200000,
	}
	for _, r := range variants {
		bw := bandwidths[r.Label]
		if bw == 0 {
			bw = 2000000
		}
		res := ""
		if r.Width > 0 {
			res = fmt.Sprintf(",RESOLUTION=%dx%d", r.Width, r.Height)
		}
		sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d%s\n%s/index.m3u8\n\n",
			bw, res, r.Label))
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func probeDuration(input string) (int, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	).Output()
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return int(f), err
}

func probeWidth(input string) (int, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	).Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func doubleRate(rate string) string {
	// e.g. "800k" → "1600k"
	if strings.HasSuffix(rate, "k") {
		n, _ := strconv.Atoi(rate[:len(rate)-1])
		return fmt.Sprintf("%dk", n*2)
	}
	return rate
}
