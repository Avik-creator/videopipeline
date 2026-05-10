# VidPipe — Self-Hosted Video Pipeline

A YouTube-like video platform built in Go. No S3, no cloud. Everything runs on your own disk.

## Architecture

```
Client (browser)
  │
  ├─ Upload ─→ Go API (chi router)
  │              │
  │              ├─ Write raw file to disk (videos/raw/)
  │              └─ Publish job to in-process channel
  │
  └─ Watch ──→ Serve HLS from disk (videos/hls/)
                  via built-in file server + CDN-friendly headers

Transcode Workers (goroutine pool)
  │
  ├─ Consume job from channel
  ├─ Run ffmpeg → 360p / 720p / 1080p HLS segments
  ├─ Write thumbnail
  ├─ Update SQLite (status = 'ready', hls_url, duration)
  └─ Delete raw file (optional)
```

## Directory layout

```
videopipe/
├── cmd/server/main.go          ← Entry point, routing, flags
├── internal/
│   ├── api/
│   │   ├── handler.go          ← GET /api/videos, SSE status stream
│   │   └── worker.go           ← Goroutine pool, transcode job runner
│   ├── db/db.go                ← SQLite (videos table, CRUD)
│   ├── storage/storage.go      ← Disk I/O, chunk/resume support
│   ├── transcode/transcode.go  ← ffmpeg wrapper, HLS output, thumbnails
│   └── upload/handler.go       ← Init, chunked upload, resume
├── frontend/index.html         ← Self-contained SPA (no build step)
├── Makefile
└── go.mod
```

## Quick start

### Prerequisites
- Go 1.22+
- ffmpeg + ffprobe  (`brew install ffmpeg` or `apt install ffmpeg`)

### Run

```bash
# Install Go deps + run dev server
make run

# Or with custom settings
go run ./cmd/server \
  -addr    :8080      \   # listen address
  -data    ./videos   \   # video storage directory
  -db      ./videopipe.db \
  -workers 4              # transcode worker count (default: NumCPU)
```

Open http://localhost:8080

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/api/videos` | List all videos (`?limit=20&offset=0`) |
| `POST` | `/api/videos/init` | Create upload session, get `video_id` |
| `GET`  | `/api/videos/{id}` | Get video metadata + HLS URL |
| `PUT`  | `/api/videos/{id}/upload` | Simple full-file upload |
| `PUT`  | `/api/videos/{id}/chunk` | Chunked upload (`Content-Range` header) |
| `GET`  | `/api/videos/{id}/resume` | Get resume byte offset |
| `GET`  | `/api/videos/{id}/status/stream` | SSE stream for processing status |
| `GET`  | `/hls/{id}/master.m3u8` | HLS master playlist |
| `GET`  | `/hls/{id}/{quality}/index.m3u8` | Quality variant playlist |
| `GET`  | `/thumbs/{id}.jpg` | Thumbnail |

## Chunked Upload Flow

```
POST /api/videos/init        → { video_id, chunk_url, resume_bytes }
GET  /api/videos/{id}/resume → { resume_bytes: N }   ← skip if resuming

# Repeat for each 5MB chunk:
PUT  /api/videos/{id}/chunk
     Content-Range: bytes 0-5242879/52428800
     Body: <binary chunk>
     → { complete: false, total_received: 5242880 }

# Last chunk:
     → { complete: true }  ← triggers transcoding
```

## Video Status Lifecycle

```
pending → processing → ready
                    ↘ failed
```

Subscribe to SSE for live updates:
```js
const src = new EventSource(`/api/videos/${id}/status/stream`);
src.onmessage = e => {
  const v = JSON.parse(e.data);
  if (v.status === 'ready') { /* load player */ }
};
```

## HLS Output Structure

```
videos/
├── raw/
│   └── {id}.mp4              ← deleted after transcoding (configurable)
├── hls/
│   └── {id}/
│       ├── master.m3u8       ← adaptive bitrate master playlist
│       ├── 360p/
│       │   ├── index.m3u8
│       │   ├── seg000.ts
│       │   └── seg001.ts ...
│       ├── 720p/
│       └── 1080p/
└── thumbs/
    └── {id}.jpg
```

## Scaling Notes

- **More workers**: increase `-workers` flag (default = `runtime.NumCPU()`)
- **Disk pressure**: raw files are deleted after transcoding; HLS segments are cache-immutable (1 year `Cache-Control`)
- **Multiple machines**: replace the in-process channel with Redis/NATS/SQS; the worker code is identical
- **CDN**: put Nginx or Cloudflare in front of `/hls/` — segments are content-addressed and immutable
