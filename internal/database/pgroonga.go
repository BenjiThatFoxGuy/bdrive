package database

import (
	"gorm.io/gorm"
)

// HasPGroonga reports whether the pgroonga extension is installed in the
// current database. It is a best-effort check: any error means "not usable",
// so callers fall back to plain ILIKE / native regex search.
func HasPGroonga(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	var one int
	if err := db.Raw("SELECT 1 FROM pg_extension WHERE extname = 'pgroonga'").Scan(&one).Error; err != nil {
		return false
	}
	return one == 1
}
