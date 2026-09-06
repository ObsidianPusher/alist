package model

import "time"

type DocsHistory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Path      string    `json:"path" gorm:"index"`
	Revision  string    `json:"revision" gorm:"index"`
	Action    string    `json:"action"`
	Snapshot  []byte    `json:"-"`
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
}
