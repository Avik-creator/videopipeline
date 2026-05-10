package db

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Video struct {
	ID         string
	UserID     string
	Title      string
	Status     string
	HLSPath    string
	ThumbPath  string
	DurationS  int
	Views      int64
	FileSize   int64
	Resolution string
	CreatedAt  time.Time
	ErrorMsg   string
}

type DB struct{ conn *sql.DB }

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, err
	}
	d := &DB{conn}
	return d, d.migrate()
}

func (d *DB) migrate() error {
	_, err := d.conn.Exec(`
	CREATE TABLE IF NOT EXISTS videos (
		id          TEXT PRIMARY KEY,
		user_id     TEXT NOT NULL DEFAULT 'anonymous',
		title       TEXT NOT NULL,
		status      TEXT NOT NULL DEFAULT 'pending',
		hls_path    TEXT,
		thumb_path  TEXT,
		duration_s  INTEGER DEFAULT 0,
		views       INTEGER DEFAULT 0,
		file_size   INTEGER DEFAULT 0,
		resolution  TEXT,
		error_msg   TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_videos_status  ON videos(status);
	CREATE INDEX IF NOT EXISTS idx_videos_created ON videos(created_at DESC);
	`)
	return err
}

func (d *DB) CreateVideo(v *Video) error {
	_, err := d.conn.Exec(
		`INSERT INTO videos (id, user_id, title, status, file_size) VALUES (?,?,?,?,?)`,
		v.ID, "anonymous", v.Title, "pending", v.FileSize,
	)
	return err
}

func (d *DB) SetStatus(id, status, errMsg string) error {
	_, err := d.conn.Exec(`UPDATE videos SET status=?, error_msg=? WHERE id=?`, status, errMsg, id)
	return err
}

func (d *DB) SetReady(id, hlsPath, thumbPath string, durationS int, resolution string) error {
	_, err := d.conn.Exec(
		`UPDATE videos SET status='ready', hls_path=?, thumb_path=?, duration_s=?, resolution=? WHERE id=?`,
		hlsPath, thumbPath, durationS, resolution, id,
	)
	return err
}

func (d *DB) Get(id string) (*Video, error) {
	v := &Video{}
	err := d.conn.QueryRow(
		`SELECT id,user_id,title,status,COALESCE(hls_path,''),COALESCE(thumb_path,''),
		        duration_s,views,file_size,COALESCE(resolution,''),COALESCE(error_msg,''),created_at
		 FROM videos WHERE id=?`, id,
	).Scan(&v.ID, &v.UserID, &v.Title, &v.Status, &v.HLSPath, &v.ThumbPath,
		&v.DurationS, &v.Views, &v.FileSize, &v.Resolution, &v.ErrorMsg, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (d *DB) List(limit, offset int) ([]*Video, error) {
	rows, err := d.conn.Query(
		`SELECT id,user_id,title,status,COALESCE(hls_path,''),COALESCE(thumb_path,''),
		        duration_s,views,file_size,COALESCE(resolution,''),created_at
		 FROM videos ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Video
	for rows.Next() {
		v := &Video{}
		if err := rows.Scan(&v.ID, &v.UserID, &v.Title, &v.Status, &v.HLSPath, &v.ThumbPath,
			&v.DurationS, &v.Views, &v.FileSize, &v.Resolution, &v.CreatedAt); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) IncrViews(id string) {
	d.conn.Exec(`UPDATE videos SET views = views + 1 WHERE id=?`, id)
}

func (d *DB) PendingJobs() ([]*Video, error) {
	rows, err := d.conn.Query(
		`SELECT id, title FROM videos WHERE status IN ('pending','processing') ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Video
	for rows.Next() {
		v := &Video{}
		rows.Scan(&v.ID, &v.Title)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) Close() error { return d.conn.Close() }
