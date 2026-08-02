package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/appcontext"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/pkg/mapper"
	"github.com/tgdrive/teldrive/pkg/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrShareNotFound    = errors.New("share not found")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrEmptyAuth        = errors.New("empty auth")
	ErrShareExpired     = errors.New("share expired")
	ErrShortCodeTaken   = errors.New("shortlink code is already in use")
	ErrShortCodeInvalid = errors.New("invalid shortlink code")
)

const (
	// shortCodeAlphabet is for auto-generated codes only, so it sticks to
	// plain alphanumerics (excluding 0/O, 1/l/I - visually ambiguous) rather
	// than the wider set validateShortCode accepts for custom codes: a random
	// code with dots or dashes scattered through it reads as broken, not
	// stylish.
	shortCodeAlphabet = "23456789abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"
	// shortCodeMinLength has nothing to do with uuid safety (see
	// shortCodePattern and the isUUID check in validateShortCode below) - it's
	// just a floor under how short a "shortlink" is useful.
	shortCodeMinLength = 4
	// shortCodeMaxLength used to be the thing standing between a custom code
	// and uuid-format ambiguity (32 < a real uuid's 36 characters), which
	// ruled out a whole class of legitimate codes along with it - notably a
	// file's own name used as its shortlink, which routinely runs longer than
	// that. Now that validateShortCode rejects a uuid-shaped code directly
	// and precisely, the length only needs to be a generous sanity bound: 255
	// matches the max filename length on most filesystems, so any real
	// filename fits without truncation.
	shortCodeMaxLength  = 255
	shortCodeGenRetries = 5
)

// shortCodePattern allows letters, numbers, dots, dashes and underscores -
// the common "slug" charset - but requires the first and last character to
// be alphanumeric. That's not just cosmetic: a code ending in "." is exactly
// the kind of thing some chat apps and link-preview bots trim as trailing
// punctuation when auto-linking a pasted URL, silently truncating the code
// the visitor actually needs.
var shortCodePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// reservedShortCodes blocks custom slugs that would be confusing or collide
// with real app routes/assets.
var reservedShortCodes = map[string]bool{
	"share": true, "shares": true, "api": true, "files": true, "auth": true,
	"admin": true, "static": true, "assets": true, "config": true, "login": true,
	"favicon.ico": true, "robots.txt": true, "health": true,
}

func validateShortCode(code string) error {
	if len(code) < shortCodeMinLength || len(code) > shortCodeMaxLength {
		return fmt.Errorf("%w: must be between %d and %d characters", ErrShortCodeInvalid, shortCodeMinLength, shortCodeMaxLength)
	}
	if !shortCodePattern.MatchString(code) {
		return fmt.Errorf("%w: only letters, numbers, dots, dashes and underscores are allowed, and it must start and end with a letter or number", ErrShortCodeInvalid)
	}
	// shareGetById (pkg/services/share.go) decides whether an incoming id is a
	// canonical uuid or a shortlink code by trying to parse it as a uuid
	// first. A custom code that itself happens to be valid uuid syntax would
	// route to the wrong lookup and never resolve - reject it outright rather
	// than let someone create a shortlink that can never work.
	if isUUID(code) {
		return fmt.Errorf("%w: can't be a uuid", ErrShortCodeInvalid)
	}
	if reservedShortCodes[strings.ToLower(code)] {
		return fmt.Errorf("%w: this code is reserved", ErrShortCodeInvalid)
	}
	return nil
}

// generateShortCode returns a random code from shortCodeAlphabet. It carries
// no uniqueness guarantee on its own — callers either retry on a unique-
// constraint conflict (create/edit) or pre-check availability (suggestions).
func generateShortCode(length int) (string, error) {
	if length <= 0 {
		length = 7
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = shortCodeAlphabet[int(b)%len(shortCodeAlphabet)]
	}
	return string(out), nil
}

// suggestUniqueShortCode returns a randomly generated code that is not
// currently in use, for prefilling the create-share dialog. It is a
// suggestion only, not a reservation — the create/edit call still handles a
// unique-constraint conflict if the code was taken in the meantime.
func (a *apiService) suggestUniqueShortCode(length int) (string, error) {
	for attempt := 0; attempt < shortCodeGenRetries; attempt++ {
		code, err := generateShortCode(length)
		if err != nil {
			return "", err
		}
		var count int64
		if err := a.db.Model(&models.FileShare{}).Where("short_code = ?", code).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", errors.New("failed to generate a unique shortlink code")
}

type fileShare struct {
	models.FileShare
	Type api.FileShareInfoType
	Name string
	Path string
}

// shareGetByColumn looks up a share by the given column, which must be one
// of the hardcoded literals below — never user input — since it's
// interpolated directly into the query.
func (a *apiService) shareGetByColumn(column, value string) (*fileShare, error) {
	var result []struct {
		models.FileShare
		Type api.FileShareInfoType `gorm:"column:type"`
		Name string                `gorm:"column:name"`
	}

	if err := a.db.Model(&models.FileShare{}).Where("file_shares."+column+" = ?", value).
		Select("file_shares.*", "f.type", "f.name").
		Joins("left join teldrive.files as f on f.id = file_shares.file_id").
		Scan(&result).Error; err != nil {
		return nil, &apiError{err: err}
	}

	if len(result) == 0 {
		return nil, &apiError{err: ErrShareNotFound, code: http.StatusNotFound}
	}

	if result[0].ExpiresAt != nil && result[0].ExpiresAt.Before(time.Now().UTC()) {
		return nil, &apiError{err: ErrShareExpired, code: http.StatusNotFound}
	}

	path, err := a.getFullPath(a.db, result[0].FileId)
	if err != nil {
		return nil, &apiError{err: err}
	}

	return &fileShare{
		FileShare: result[0].FileShare,
		Type:      result[0].Type,
		Name:      result[0].Name,
		Path:      path,
	}, nil
}

// shareGetById resolves either a legacy uuid share id or a shortlink code —
// the shortlink alphabet never contains dashes, so a canonical (dashed) uuid
// can never collide with a real code.
func (a *apiService) shareGetById(idOrCode string) (*fileShare, error) {
	if isUUID(idOrCode) {
		return a.shareGetByColumn("id", idOrCode)
	}
	return a.shareGetByColumn("short_code", idOrCode)
}

// shareGetByCode resolves a shortlink code only — unlike shareGetById it
// never falls back to matching a bare uuid, which is what lets the
// shortlink resolver tell "this is a real short code" apart from "this is
// just a legacy uuid share link typed into the same URL slot".
func (a *apiService) shareGetByCode(code string) (*fileShare, error) {
	return a.shareGetByColumn("short_code", code)
}

func (a *apiService) SharesGetById(ctx context.Context, params api.SharesGetByIdParams) (*api.FileShareInfo, error) {
	share, err := a.shareGetById(params.ID)

	if err != nil {
		return nil, err
	}
	res := &api.FileShareInfo{
		Protected: share.Password != nil,
		UserId:    share.UserId,
		Type:      share.Type,
		Name:      share.Name,
	}
	if share.ExpiresAt != nil {
		res.ExpiresAt = api.NewOptDateTime(*share.ExpiresAt)
	}
	if share.ShortCode != nil {
		res.ShortCode = api.NewOptString(*share.ShortCode)
	}
	res.AllowZipDownload = api.NewOptBool(share.AllowZipDownload)
	return res, nil
}

func (a *apiService) SharesUnlock(ctx context.Context, req *api.ShareUnlock, params api.SharesUnlockParams) error {
	share, err := a.shareGetById(params.ID)
	if err != nil {
		return err
	}

	if share.Password == nil {
		return &apiError{err: ErrInvalidPassword, code: http.StatusForbidden}
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*share.Password), []byte(req.Password)); err != nil {
		return &apiError{err: ErrInvalidPassword, code: http.StatusForbidden}
	}
	return nil
}

func (a *apiService) SharesListFiles(ctx context.Context, params api.SharesListFilesParams) (*api.FileList, error) {
	c := ctx.(*appcontext.Context)
	share, err := a.validFileShare(c.Request, params.ID)
	if err != nil {
		return nil, err
	}
	fileType := share.Type

	if fileType == api.FileShareInfoTypeFolder {
		queryBuilder := &fileQueryBuilder{db: a.db}
		return queryBuilder.execute(&api.FilesListParams{
			Path:      api.NewOptString(share.Path + params.Path.Or("")),
			Limit:     params.Limit,
			Page:      params.Page,
			Status:    api.NewOptFileQueryStatus(api.FileQueryStatusActive),
			Order:     api.NewOptFileQueryOrder(api.FileQueryOrder(string(params.Order.Value))),
			Sort:      api.NewOptFileQuerySort(api.FileQuerySort(string(params.Sort.Value))),
			Operation: api.NewOptFileQueryOperation(api.FileQueryOperationList)}, share.UserId)
	} else {
		var file models.File
		if err := a.db.Where("id = ?", share.FileId).First(&file).Error; err != nil {
			if database.IsRecordNotFoundErr(err) {
				return nil, &apiError{err: database.ErrNotFound, code: http.StatusNotFound}
			}
			return nil, &apiError{err: err}
		}
		return &api.FileList{Items: []api.File{*mapper.ToFileOut(file)},
			Meta: api.Meta{Count: 1, TotalPages: 1, CurrentPage: 1}}, nil
	}

}
func (a *apiService) validFileShare(r *http.Request, id string) (*fileShare, error) {

	share, err := cache.FetchArg(r.Context(), a.cache, cache.KeyShare(id), 0, a.shareGetById, id)

	if err != nil {
		return nil, &apiError{err: err}
	}

	if share.Password != nil {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			return nil, &apiError{err: ErrEmptyAuth, code: http.StatusUnauthorized}
		}
		bytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
		if err != nil {
			return nil, &apiError{err: err}
		}
		password := strings.Split(string(bytes), ":")[1]

		if err := bcrypt.CompareHashAndPassword([]byte(*share.Password), []byte(password)); err != nil {
			return nil, &apiError{err: ErrInvalidPassword, code: http.StatusUnauthorized}
		}

	}
	return share, nil
}
