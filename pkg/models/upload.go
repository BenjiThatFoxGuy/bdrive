package models

import (
	"time"

	"gorm.io/datatypes"
)

type Upload struct {
	UploadId    string    `gorm:"type:text"`
	UserId      int64     `gorm:"type:bigint"`
	Name        string    `gorm:"type:text"`
	PartNo      int       `gorm:"type:integer"`
	PartId      int       `gorm:"type:integer"`
	Encrypted   bool      `gorm:"default:false"`
	Salt        string    `gorm:"type:text"`
	BlockHashes []byte    `gorm:"type:bytea"` // 16MB block hashes for tree hashing
	ChannelId   int64     `gorm:"type:bigint"`
	Size        int64     `gorm:"type:bigint"`
	CreatedAt   time.Time `gorm:"default:timezone('utc'::text, now())"`
}

// CommittedPart is the part metadata kept after an upload session is folded
// into a file. It is enough to answer a replayed part upload from the database
// instead of sending the bytes to Telegram a second time.
type CommittedPart struct {
	PartNo      int    `json:"partNo"`
	PartId      int    `json:"partId"`
	Name        string `json:"name"`
	ChannelId   int64  `json:"channelId"`
	Size        int64  `json:"size"`
	Encrypted   bool   `json:"encrypted"`
	Salt        string `json:"salt,omitempty"`
	BlockHashes []byte `json:"blockHashes,omitempty"`
}

// UploadCommit records the file a completed upload session produced. The
// (user_id, upload_id) key is the same deterministic id the client derives
// from directory, name, size and user, so a retry of the same logical upload
// finds the original commit.
type UploadCommit struct {
	UserId    int64                              `gorm:"type:bigint;primaryKey"`
	UploadId  string                             `gorm:"type:text;primaryKey"`
	FileId    string                             `gorm:"type:uuid;not null"`
	FileSize  int64                              `gorm:"type:bigint;not null"`
	Parts     datatypes.JSONSlice[CommittedPart] `gorm:"type:jsonb;not null"`
	CreatedAt time.Time                          `gorm:"default:timezone('utc'::text, now())"`
}
