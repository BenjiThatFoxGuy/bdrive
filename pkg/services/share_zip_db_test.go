package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	extraClausePlugin "github.com/WinterYukky/gorm-extra-clause-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/appcontext"
	"github.com/tgdrive/teldrive/internal/auth"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/internal/database"
	"github.com/tgdrive/teldrive/internal/tgc"
	"github.com/tgdrive/teldrive/pkg/models"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const shareZipTestUserId = int64(987654321)

// openShareZipTestDB connects to the database named by TELDRIVE_DB_DATASOURCE
// and migrates it. Skips when the variable is unset so the rest of the package's
// tests keep running without a database.
func openShareZipTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TELDRIVE_DB_DATASOURCE")
	if dsn == "" {
		t.Skip("TELDRIVE_DB_DATASOURCE not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "teldrive.", SingularTable: false},
		Logger:         logger.Default.LogMode(logger.Silent),
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	require.NoError(t, err)
	db.Use(extraClausePlugin.New())

	require.NoError(t, database.MigrateDB(db))

	// Each test builds its own tree, and files are unique per (name, parent), so
	// start from a clean slate rather than colliding with the last run.
	require.NoError(t, db.Exec("DELETE FROM teldrive.files WHERE user_id = ?", shareZipTestUserId).Error)

	return db
}

// mkdir inserts a folder and returns its id.
func mkdir(t *testing.T, db *gorm.DB, name string, parentId *string) string {
	t.Helper()
	now := time.Now().UTC()
	folder := models.File{
		Name: name, Type: "folder", MimeType: "drive/folder",
		UserId: shareZipTestUserId, Status: "active", ParentId: parentId,
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&folder).Error)
	return folder.ID
}

// mkfile inserts a file and returns its id.
func mkfile(t *testing.T, db *gorm.DB, name string, parentId string, size int64) string {
	t.Helper()
	now := time.Now().UTC()
	file := models.File{
		Name: name, Type: "file", MimeType: "text/plain", Size: &size,
		UserId: shareZipTestUserId, Status: "active", ParentId: &parentId,
		CreatedAt: &now, UpdatedAt: &now,
	}
	require.NoError(t, db.Create(&file).Error)
	return file.ID
}

// TestAssertIdsWithinShareDB is the regression test for the share scope hole:
// SharesDownloadZip used to hand the caller's ids straight to
// collectFilesRecursive, so a share viewer could name any file the share owner
// happened to own and have it zipped for them.
func TestAssertIdsWithinShareDB(t *testing.T) {
	db := openShareZipTestDB(t)

	// A tree the "root" folder walk in getFullPath can terminate on.
	rootId := mkdir(t, db, "root", nil)
	sharedId := mkdir(t, db, "Shared", &rootId)
	nestedId := mkdir(t, db, "Nested", &sharedId)
	privateId := mkdir(t, db, "Private", &rootId)

	insideId := mkfile(t, db, "inside.txt", sharedId, 10)
	deepId := mkfile(t, db, "deep.txt", nestedId, 10)
	outsideId := mkfile(t, db, "secret.txt", privateId, 10)

	a := &apiService{db: db}

	folderShare := &fileShare{Type: api.FileShareInfoTypeFolder, Name: "Shared", Path: "/Shared"}
	folderShare.FileId = sharedId
	folderShare.UserId = shareZipTestUserId

	fileShareOnly := &fileShare{Type: api.FileShareInfoTypeFile, Name: "inside.txt", Path: "/Shared/inside.txt"}
	fileShareOnly.FileId = insideId
	fileShareOnly.UserId = shareZipTestUserId

	tests := []struct {
		name    string
		share   *fileShare
		ids     []string
		wantErr bool
	}{
		{name: "the shared folder itself", share: folderShare, ids: []string{sharedId}},
		{name: "a file directly inside the share", share: folderShare, ids: []string{insideId}},
		{name: "a file nested deeper in the share", share: folderShare, ids: []string{deepId}},
		{name: "a subfolder of the share", share: folderShare, ids: []string{nestedId}},
		{name: "several ids all inside the share", share: folderShare, ids: []string{insideId, deepId, nestedId}},

		{name: "a file the owner owns but did not share", share: folderShare, ids: []string{outsideId}, wantErr: true},
		{name: "a sibling folder outside the share", share: folderShare, ids: []string{privateId}, wantErr: true},
		{name: "one bad id among good ones", share: folderShare, ids: []string{insideId, outsideId}, wantErr: true},
		{name: "an id that does not exist", share: folderShare, ids: []string{"00000000-0000-0000-0000-000000000000"}, wantErr: true},

		{name: "the shared file itself", share: fileShareOnly, ids: []string{insideId}},
		{name: "a different file through a single-file share", share: fileShareOnly, ids: []string{outsideId}, wantErr: true},
		{name: "a sibling file through a single-file share", share: fileShareOnly, ids: []string{deepId}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.assertIdsWithinShare(db, tt.share, tt.ids)
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *apiError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 403, apiErr.code)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestGetFullPathMatchesSharePath pins the assumption assertIdsWithinShare
// rests on: the paths it compares are produced the same way share.Path is.
func TestGetFullPathMatchesSharePath(t *testing.T) {
	db := openShareZipTestDB(t)

	rootId := mkdir(t, db, "root", nil)
	sharedId := mkdir(t, db, "Holiday", &rootId)
	nestedId := mkdir(t, db, "2024", &sharedId)
	fileId := mkfile(t, db, "beach.jpg", nestedId, 10)

	a := &apiService{db: db}

	sharePath, err := a.getFullPath(db, sharedId)
	require.NoError(t, err)
	assert.Equal(t, "/Holiday", sharePath)

	filePath, err := a.getFullPath(db, fileId)
	require.NoError(t, err)
	assert.Equal(t, "/Holiday/2024/beach.jpg", filePath)

	assert.True(t, pathWithinShare(sharePath, filePath))
}

// mkshare inserts a share over fileId and returns its id.
func mkshare(t *testing.T, db *gorm.DB, fileId string, password *string) string {
	t.Helper()
	share := models.FileShare{FileId: fileId, UserId: shareZipTestUserId, Password: password}
	require.NoError(t, db.Create(&share).Error)
	return share.ID
}

// newShareZipTestServer wires the real ogen server the way cmd/run.go does, so
// requests go through the generated router, request decoder and security
// handler rather than calling the service method directly.
func newShareZipTestServer(t *testing.T, db *gorm.DB, cnf *config.ServerCmdConfig) http.Handler {
	t.Helper()

	c := cache.NewCache(context.Background(), 10485760, nil, zap.NewNop())
	svc := &apiService{
		db:             db,
		cnf:            cnf,
		cache:          c,
		channelManager: tgc.NewChannelManager(db, c, &cnf.TG),
		dedup:          newDedupManager(),
	}
	if limit := cnf.Files.ZipMaxConcurrent; limit > 0 {
		svc.zipSlots = make(chan struct{}, limit)
	}

	srv, err := api.NewServer(svc, auth.NewSecurityHandler(db, c, &cnf.JWT))
	require.NoError(t, err)

	return appcontext.Middleware(http.StripPrefix("/api", srv))
}

func zipForm(ids ...string) io.Reader {
	form := url.Values{}
	for _, id := range ids {
		form.Add("ids", id)
	}
	return strings.NewReader(form.Encode())
}

// TestSharesDownloadZipHTTP drives the endpoint over real HTTP. It covers the
// pieces that only exist once the generated code is in play: decoding the
// form-encoded body, and rejecting out-of-share ids before anything reaches
// Telegram.
func TestSharesDownloadZipHTTP(t *testing.T) {
	db := openShareZipTestDB(t)
	require.NoError(t, db.Exec("DELETE FROM teldrive.file_shares WHERE user_id = ?", shareZipTestUserId).Error)

	rootId := mkdir(t, db, "root", nil)
	sharedId := mkdir(t, db, "Shared", &rootId)
	privateId := mkdir(t, db, "Private", &rootId)
	insideId := mkfile(t, db, "inside.txt", sharedId, 10)
	outsideId := mkfile(t, db, "secret.txt", privateId, 10)

	shareId := mkshare(t, db, sharedId, nil)

	cnf := &config.ServerCmdConfig{}
	cnf.Files = config.FilesConfig{EnableZipDownload: true, ZipMaxFiles: 10000, ZipMaxConcurrent: 4}
	handler := newShareZipTestServer(t, db, cnf)

	post := func(t *testing.T, path string, body io.Reader) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	t.Run("an id outside the share is refused", func(t *testing.T) {
		res := post(t, "/api/shares/"+shareId+"/zip", zipForm(outsideId))
		assert.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("a good id alongside a bad one is refused", func(t *testing.T) {
		res := post(t, "/api/shares/"+shareId+"/zip", zipForm(insideId, outsideId))
		assert.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	t.Run("the form body decodes and gets past scoping", func(t *testing.T) {
		// The owner has neither bots nor a stored session here, so this stops at
		// the Telegram client. What matters is that it got that far: the ids were
		// decoded from the form and accepted by the scope check.
		res := post(t, "/api/shares/"+shareId+"/zip", zipForm(insideId))
		assert.NotEqual(t, http.StatusBadRequest, res.StatusCode)
		body, _ := io.ReadAll(res.Body)
		assert.NotContains(t, string(body), "not part of this share")
	})

	t.Run("an empty body is rejected", func(t *testing.T) {
		res := post(t, "/api/shares/"+shareId+"/zip", strings.NewReader(""))
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("a JSON body is no longer accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/shares/"+shareId+"/zip",
			strings.NewReader(`{"ids":["`+insideId+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	})

	t.Run("disabled by config", func(t *testing.T) {
		off := &config.ServerCmdConfig{}
		off.Files = config.FilesConfig{EnableZipDownload: false}
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/shares/"+shareId+"/zip", zipForm(insideId))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		newShareZipTestServer(t, db, off).ServeHTTP(res, req)
		assert.Equal(t, http.StatusForbidden, res.Code)
	})
}

// TestSharesDownloadZipPasswordPrompt pins the WWW-Authenticate header that lets
// the browser prompt for a protected share's password. The UI submits this
// request as a form navigation and cannot set the header itself.
func TestSharesDownloadZipPasswordPrompt(t *testing.T) {
	db := openShareZipTestDB(t)
	require.NoError(t, db.Exec("DELETE FROM teldrive.file_shares WHERE user_id = ?", shareZipTestUserId).Error)

	rootId := mkdir(t, db, "root", nil)
	sharedId := mkdir(t, db, "Shared", &rootId)
	insideId := mkfile(t, db, "inside.txt", sharedId, 10)

	hashed, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.DefaultCost)
	require.NoError(t, err)
	password := string(hashed)
	shareId := mkshare(t, db, sharedId, &password)

	cnf := &config.ServerCmdConfig{}
	cnf.Files = config.FilesConfig{EnableZipDownload: true}
	handler := newShareZipTestServer(t, db, cnf)

	req := httptest.NewRequest(http.MethodPost, "/api/shares/"+shareId+"/zip", zipForm(insideId))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, `Basic realm="Restricted"`, rec.Header().Get("WWW-Authenticate"))
}
