package routing

import "time"

type VideoJob struct {
	ID           string    `json:"id"`
	SourcePath   string    `json:"source_path"`
	TargetFormat string    `json:"target_format"`
	UserID       string    `json:"user_id"`
	CreatedAt    time.Time `json:"created_at"`
}

const (
	ExchangeVideoTopic = "video_topic"
	VideoUploadKey     = "video.upload"
	VideoQueue         = "video_processing"
)
