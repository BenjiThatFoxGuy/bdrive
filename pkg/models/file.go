package models

import (
	"errors"
	"time"

	"gorm.io/datatypes"

	"github.com/tgdrive/teldrive/internal/api"
)

type File struct {
	ID               string                         `gorm:"type:uuid;primaryKey;default:uuid7()"`
	Name             string                         `gorm:"type:text;not null"`
	Type             string                         `gorm:"type:text;not null"`
	MimeType         string                         `gorm:"type:text;not null"`
	Size             *int64                         `gorm:"type:bigint"`
	Category         *string                        `gorm:"type:text"`
	Encrypted        *bool                          `gorm:"default:false"`
	Starred          *bool                          `gorm:"default:false"`
	UserId           int64                          `gorm:"type:bigint;not null"`
	Status           string                         `gorm:"type:text"`
	ParentId         *string                        `gorm:"type:uuid;index"`
	Parts            *datatypes.JSONSlice[api.Part] `gorm:"type:jsonb"`
	ChannelId        *int64                         `gorm:"type:bigint"`
	Hash             *string                        `gorm:"type:text"`       // BLAKE3 tree hash
	ReferencedFileId *string                        `gorm:"type:uuid;index"` // Points to canonical file if this is a deduplicated copy
	CreatedAt        *time.Time                     `gorm:"default:timezone('utc'::text, now())"`
	UpdatedAt        *time.Time                     `gorm:"autoUpdateTime:false"`
}

// IsEncrypted reports whether the file is stored encrypted. Encrypted is a
// nullable column, and a File is also read in-memory before it is persisted
// (the dedup hash backfill hashes a file that has not been inserted yet), so
// the pointer can legitimately be nil. Dereferencing it directly panics, so
// every read of the flag goes through here.
var ErrInvalidFile = errors.New("invalid file descriptor")

// ValidateStream rejects incomplete persisted entries before any dependency work.
// Zero-byte files do not need Telegram parts.
func (f *File) ValidateStream() error {
	if f == nil || f.Size == nil || *f.Size < 0 || f.Type == "folder" {
		return ErrInvalidFile
	}
	if *f.Size == 0 {
		return nil
	}
	if f.ChannelId == nil || *f.ChannelId == 0 || f.Parts == nil || len(*f.Parts) == 0 {
		return ErrInvalidFile
	}
	seen := make(map[int]bool, len(*f.Parts))
	for _, part := range *f.Parts {
		if part.ID <= 0 || seen[part.ID] || (f.IsEncrypted() && part.Salt.Value == "") {
			return ErrInvalidFile
		}
		seen[part.ID] = true
	}
	return nil
}

func (f *File) IsEncrypted() bool {
	return f.Encrypted != nil && *f.Encrypted
}
