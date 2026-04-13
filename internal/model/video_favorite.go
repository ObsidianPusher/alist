package model

import "time"

// VideoFavorite stores user-selected favorite video items for quick access.
type VideoFavorite struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	UserID      uint      `json:"user_id" gorm:"index:idx_video_fav_user_storage_path,unique;index:idx_video_fav_user_time"`
	StorageID   uint      `json:"storage_id" gorm:"index:idx_video_fav_user_storage_path,unique;index:idx_video_fav_user_storage_time"`
	StorageName string    `json:"storage_name"`
	Path        string    `json:"path" gorm:"size:4096;index:idx_video_fav_user_storage_path,unique"`
	Name        string    `json:"name" gorm:"index"`
	FileID      string    `json:"file_id" gorm:"index"`
	Size        int64     `json:"size"`
	ModTime     int64     `json:"mod_time"`
	CreatedAt   time.Time `json:"created_at" gorm:"index:idx_video_fav_user_time;index:idx_video_fav_user_storage_time"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}
