package services

import (
	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/apperr"
)

func fileNotFound(id uuid.UUID, cause error) error {
	return apperr.NotFound("file", id.String(), cause)
}

func periodicJobNotFound(id string, cause error) error {
	return apperr.NotFound("periodic_job", id, cause)
}

func shareNotFound(id uuid.UUID, cause error) error {
	return apperr.NotFound("share", id.String(), cause)
}
