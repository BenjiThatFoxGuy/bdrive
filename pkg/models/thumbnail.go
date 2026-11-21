package models

import "time"

type Thumbnail struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:teldrive.uuid7()"`
	FileId    string    `gorm:"type:uuid;not null;uniqueIndex"`
	UserId    int64     `gorm:"type:bigint;not null"`
	ChannelId int64     `gorm:"type:bigint;not null"`
	MessageId int       `gorm:"type:integer;not null"`
	Encrypted bool      `gorm:"default:false"`
	Salt      string    `gorm:"type:text;default:''"`
	Size      int64     `gorm:"type:bigint;default:0"`
	MimeType  string    `gorm:"type:text;default:'image/jpeg'"`
	CreatedAt time.Time `gorm:"default:timezone('utc'::text, now())"`
}

func (Thumbnail) TableName() string {
	return "teldrive.thumbnails"
}
