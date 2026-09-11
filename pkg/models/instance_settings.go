package models

import (
	"time"

	"gorm.io/datatypes"
)

type InstanceSettings struct {
	Key       string            `gorm:"type:text;primaryKey"`
	Value     datatypes.JSON    `gorm:"type:jsonb;not null;default:'{}'"`
	UpdatedAt time.Time         `gorm:"default:timezone('utc'::text, now())"`
}

func (InstanceSettings) TableName() string {
	return "teldrive.instance_settings"
}

// ThumbnailConfig is the JSON structure stored under the "thumbnail" key.
type ThumbnailConfig struct {
	ResizerHost    string `json:"resizerHost"`
	ResizerWidth   int    `json:"resizerWidth"`
	ResizerQuality int    `json:"resizerQuality"`
}
