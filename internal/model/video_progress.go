package model

import "time"

type VideoProgress struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	StorageID uint      `json:"storage_id" gorm:"index:idx_video_progress_storage_path,unique"`
	Path      string    `json:"path" gorm:"index:idx_video_progress_storage_path,unique"`
	FileID    string    `json:"file_id" gorm:"index"`
	Size      int64     `json:"size"`
	ModTime   int64     `json:"mod_time"`
	Duration  float64   `json:"duration"`
	Position  float64   `json:"position"`
	Finished  bool      `json:"finished" gorm:"index"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}
