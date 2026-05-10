package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store manages all file I/O on local disk.
type Store struct {
	RawDir   string // raw uploaded files
	HLSDir   string // transcoded HLS output
	ThumbDir string // thumbnails
}

func New(base string) (*Store, error) {
	s := &Store{
		RawDir:   filepath.Join(base, "raw"),
		HLSDir:   filepath.Join(base, "hls"),
		ThumbDir: filepath.Join(base, "thumbs"),
	}
	for _, d := range []string{s.RawDir, s.HLSDir, s.ThumbDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return s, nil
}

// RawPath returns the full disk path for a raw upload.
func (s *Store) RawPath(videoID string) string {
	return filepath.Join(s.RawDir, videoID+".mp4")
}

// HLSVideoDir returns (and creates) the HLS output directory for a video.
func (s *Store) HLSVideoDir(videoID string) (string, error) {
	d := filepath.Join(s.HLSDir, videoID)
	return d, os.MkdirAll(d, 0755)
}

// ThumbPath returns the thumbnail path for a video.
func (s *Store) ThumbPath(videoID string) string {
	return filepath.Join(s.ThumbDir, videoID+".jpg")
}

// SaveUpload writes r into the raw file for videoID.
// It appends if the file already exists (resumable chunk support).
func (s *Store) SaveUpload(videoID string, r io.Reader) (int64, error) {
	path := s.RawPath(videoID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return 0, fmt.Errorf("open raw file: %w", err)
	}
	defer f.Close()
	return io.Copy(f, r)
}

// SaveChunk writes a specific chunk at a byte offset (for parallel chunk upload).
func (s *Store) SaveChunk(videoID string, offset int64, r io.Reader) (int64, error) {
	path := s.RawPath(videoID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("open raw file: %w", err)
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.Copy(f, r)
}

// FileSize returns the current size of the raw file (for resume support).
func (s *Store) RawSize(videoID string) int64 {
	info, err := os.Stat(s.RawPath(videoID))
	if err != nil {
		return 0
	}
	return info.Size()
}

// DeleteRaw removes the raw upload file after transcoding.
func (s *Store) DeleteRaw(videoID string) error {
	return os.Remove(s.RawPath(videoID))
}
