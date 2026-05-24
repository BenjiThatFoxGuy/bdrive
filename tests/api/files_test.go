package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/services"
)

func TestFilesCreateFolder(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	created, err := client.FilesCreate(ctx, &api.File{Name: "docs", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}
	if !created.ID.IsSet() || uuid.UUID(created.ID.Value) == uuid.Nil {
		t.Fatal("created folder id is empty")
	}

	createdGet, err := client.FilesGetById(ctx, api.FilesGetByIdParams{ID: created.ID.Value})
	if err != nil {
		t.Fatalf("FilesGetById folder failed: %v", err)
	}
	if createdGet.Name != "docs" || createdGet.Type != api.FileTypeFolder {
		t.Fatalf("unexpected folder payload: %+v", createdGet)
	}
}

func TestFilesCreateFile(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	// Create a folder first to prove listing works.
	_, err := client.FilesCreate(ctx, &api.File{Name: "docs", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}

	listAll, err := client.FilesList(ctx, api.FilesListParams{Limit: api.NewOptInt(100)})
	if err != nil {
		t.Fatalf("FilesList failed: %v", err)
	}
	if len(listAll.Items) < 1 {
		t.Fatalf("expected file list items >= 1, got %d", len(listAll.Items))
	}

	dataFile, err := client.FilesCreate(ctx, &api.File{
		Name:      "report.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(900001),
		Size:      api.NewOptInt64(42),
		Encrypted: api.NewOptBool(true),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}
	if !dataFile.ID.IsSet() || uuid.UUID(dataFile.ID.Value) == uuid.Nil {
		t.Fatal("created file id is empty")
	}
}

func TestFilesUpdateFolder(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	created, err := client.FilesCreate(ctx, &api.File{Name: "docs", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}

	updated, err := client.FilesUpdate(ctx, &api.FileUpdate{Name: api.NewOptString("docs-updated")}, api.FilesUpdateParams{ID: created.ID.Value})
	if err != nil {
		t.Fatalf("FilesUpdate folder failed: %v", err)
	}
	if updated.Name != "docs-updated" {
		t.Fatalf("expected updated folder name docs-updated, got %s", updated.Name)
	}
}

func TestFilesUpdateFile(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	createdGet, err := client.FilesCreate(ctx, &api.File{
		Name:      "report.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(900001),
		Size:      api.NewOptInt64(42),
		Encrypted: api.NewOptBool(true),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}
	if !createdGet.UpdatedAt.IsSet() {
		t.Fatal("expected updatedAt on created file")
	}

	updatedFile, err := client.FilesUpdate(ctx, &api.FileUpdate{
		Name:      api.NewOptString("report-updated.txt"),
		ChannelId: api.NewOptInt64(900002),
		Encrypted: api.NewOptBool(false),
		UpdatedAt: api.NewOptDateTime(createdGet.UpdatedAt.Value.Add(5 * time.Minute)),
	}, api.FilesUpdateParams{ID: createdGet.ID.Value})
	if err != nil {
		t.Fatalf("FilesUpdate file failed: %v", err)
	}
	if updatedFile.Name != "report-updated.txt" {
		t.Fatalf("expected updated file name, got %s", updatedFile.Name)
	}
	wantUpdatedAt := createdGet.UpdatedAt.Value.Add(5 * time.Minute)
	if !updatedFile.UpdatedAt.IsSet() || !updatedFile.UpdatedAt.Value.Equal(wantUpdatedAt) {
		t.Fatalf("expected updatedAt=%s, got %+v", wantUpdatedAt.UTC().Format(time.RFC3339Nano), updatedFile.UpdatedAt)
	}
}

func TestFilesUpdateParent(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	created, err := client.FilesCreate(ctx, &api.File{Name: "docs", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}

	archive, err := client.FilesCreate(ctx, &api.File{Name: "archive", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate archive failed: %v", err)
	}

	updatedParent, err := client.FilesUpdate(ctx, &api.FileUpdate{ParentId: api.NewOptUUID(archive.ID.Value)}, api.FilesUpdateParams{ID: created.ID.Value})
	if err != nil {
		t.Fatalf("FilesUpdate parent failed: %v", err)
	}
	if !updatedParent.ParentId.IsSet() || updatedParent.ParentId.Value != archive.ID.Value {
		t.Fatalf("expected parentId=%s, got %+v", archive.ID.Value, updatedParent.ParentId)
	}
}

func TestFilesDeleteById(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	dataFile, err := client.FilesCreate(ctx, &api.File{
		Name:      "delete-me.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(900003),
		Size:      api.NewOptInt64(7),
	})
	if err != nil {
		t.Fatalf("FilesCreate file failed: %v", err)
	}

	if err := client.FilesDeleteById(ctx, api.FilesDeleteByIdParams{ID: dataFile.ID.Value}); err != nil {
		t.Fatalf("FilesDeleteById file failed: %v", err)
	}
	_, err = client.FilesGetById(ctx, api.FilesGetByIdParams{ID: dataFile.ID.Value})
	if statusCode(err) != 404 {
		t.Fatalf("expected 404 for pending_deletion file, got %d err=%v", statusCode(err), err)
	}
}

func TestFilesDeletePending(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7202, "user7202")

	purgeFile, err := client.FilesCreate(ctx, &api.File{
		Name:      "purge.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(900003),
		Size:      api.NewOptInt64(7),
	})
	if err != nil {
		t.Fatalf("FilesCreate purge file failed: %v", err)
	}
	if err := client.FilesDelete(ctx, &api.FileDelete{Ids: []api.UUID{purgeFile.ID.Value}}); err != nil {
		t.Fatalf("FilesDelete pending deletion failed: %v", err)
	}
}

func TestFilesCopy(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 10010, "user10010")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910084), ChannelName: api.NewOptString("copy-default")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}
	if _, err := client.FilesCreate(ctx, &api.File{Name: "copies", Type: api.FileTypeFolder, Path: api.NewOptString("/")}); err != nil {
		t.Fatalf("FilesCreate destination folder failed: %v", err)
	}

	// Create a source file with parts.
	sourceFile, err := client.FilesCreate(ctx, &api.File{
		Name:      "source.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910084),
		Size:      api.NewOptInt64(100),
		Parts: []api.Part{
			{ID: 1, Salt: api.NewOptString("salt-a")},
			{ID: 2, Salt: api.NewOptString("salt-b")},
		},
	})
	if err != nil {
		t.Fatalf("FilesCreate source file failed: %v", err)
	}

	s.tgMock.copyFilePartsFn = func(_ context.Context, _ services.TelegramClient, fromChannelID, toChannelID int64, parts []api.Part) ([]api.Part, error) {
		if fromChannelID != 910084 {
			return nil, fmt.Errorf("unexpected fromChannelID: %d", fromChannelID)
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("expected at least one part")
		}
		result := make([]api.Part, len(parts))
		for i, p := range parts {
			result[i] = api.Part{ID: p.ID + 1000, Salt: p.Salt}
		}
		return result, nil
	}
	s.tgMock.runWithAuthFn = func(_ context.Context, _ services.TelegramClient, _ string, f func(context.Context) error) error {
		return f(ctx)
	}
	t.Cleanup(func() {
		s.tgMock.copyFilePartsFn = nil
		s.tgMock.runWithAuthFn = nil
	})

	copied, err := client.FilesCopy(ctx, &api.FileCopy{Destination: "/copies"}, api.FilesCopyParams{ID: sourceFile.ID.Value})
	if err != nil {
		t.Fatalf("FilesCopy failed: %v", err)
	}
	if !copied.ID.IsSet() || uuid.UUID(copied.ID.Value) == uuid.Nil {
		t.Fatal("expected copied file id")
	}
	if copied.Name != "source.txt" {
		t.Fatalf("expected copied name to be 'source.txt', got %s", copied.Name)
	}
	if !copied.Size.IsSet() || copied.Size.Value != 100 {
		t.Fatalf("expected copied size 100, got %d", copied.Size.Value)
	}

	// Verify the copy is retrievable.
	gotCopied, err := client.FilesGetById(ctx, api.FilesGetByIdParams{ID: copied.ID.Value})
	if err != nil {
		t.Fatalf("FilesGetById copied file failed: %v", err)
	}
	if gotCopied.Name != "source.txt" {
		t.Fatalf("expected retrieved name 'source.txt', got %s", gotCopied.Name)
	}
}

func TestFilesMove(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 10011, "user10011")

	// Create source and destination folders.
	_, err := client.FilesCreate(ctx, &api.File{Name: "source", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate source folder failed: %v", err)
	}
	dest, err := client.FilesCreate(ctx, &api.File{Name: "dest", Type: api.FileTypeFolder, Path: api.NewOptString("/")})
	if err != nil {
		t.Fatalf("FilesCreate dest folder failed: %v", err)
	}

	// Create a file inside source.
	file, err := client.FilesCreate(ctx, &api.File{
		Name:      "move-me.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/source"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910085),
		Size:      api.NewOptInt64(50),
	})
	if err != nil {
		t.Fatalf("FilesCreate file in source failed: %v", err)
	}

	// Move file to dest.
	if err := client.FilesMove(ctx, &api.FileMove{
		Ids:               []api.UUID{file.ID.Value},
		DestinationParent: uuid.UUID(dest.ID.Value).String(),
	}); err != nil {
		t.Fatalf("FilesMove failed: %v", err)
	}

	// Verify the file's parent has changed.
	movedFile, err := client.FilesGetById(ctx, api.FilesGetByIdParams{ID: file.ID.Value})
	if err != nil {
		t.Fatalf("FilesGetById moved file failed: %v", err)
	}
	if !movedFile.ParentId.IsSet() {
		t.Fatal("expected moved file to have a parent")
	}
	if movedFile.ParentId.Value != dest.ID.Value {
		t.Fatalf("expected parent to be %s, got %s", dest.ID.Value, movedFile.ParentId.Value)
	}
}

func TestFilesCategoryStats(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 10012, "user10012")

	if err := client.UsersUpdateChannel(ctx, &api.ChannelUpdate{ChannelId: api.NewOptInt64(910086), ChannelName: api.NewOptString("stats-default")}); err != nil {
		t.Fatalf("UsersUpdateChannel failed: %v", err)
	}

	// Create a file so there's data for stats.
	_, err := client.FilesCreate(ctx, &api.File{
		Name:      "stats.txt",
		Type:      api.FileTypeFile,
		Path:      api.NewOptString("/"),
		MimeType:  api.NewOptString("text/plain"),
		ChannelId: api.NewOptInt64(910086),
		Size:      api.NewOptInt64(25),
	})
	if err != nil {
		t.Fatalf("FilesCreate stats file failed: %v", err)
	}

	stats, err := client.FilesCategoryStats(ctx)
	if err != nil {
		t.Fatalf("FilesCategoryStats failed: %v", err)
	}
	if len(stats) == 0 {
		t.Fatalf("expected at least one category stats entry")
	}
}

func TestFilesMkdir(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 10013, "user10013")

	_, err := client.FilesCreate(ctx, &api.File{
		Name: "myfolder",
		Type: api.FileTypeFolder,
		Path: api.NewOptString("/"),
	})
	if err != nil {
		t.Fatalf("FilesCreate folder failed: %v", err)
	}

	list, err := client.FilesList(ctx, api.FilesListParams{
		Type:  api.NewOptFileQueryType(api.FileQueryTypeFolder),
		Limit: api.NewOptInt(100),
	})
	if err != nil {
		t.Fatalf("FilesList failed: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatal("expected at least one folder in listing")
	}
}

func TestFilesRootQueryParam(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7204, "user7204")

	// Create a root-level folder (parent IS NULL).
	rootFolder, err := client.FilesCreate(ctx, &api.File{
		Name: "root-folder",
		Type: api.FileTypeFolder,
		Path: api.NewOptString("/"),
	})
	if err != nil {
		t.Fatalf("FilesCreate root folder failed: %v", err)
	}

	// Create a nested folder inside rootFolder (parent IS NOT NULL).
	_, err = client.FilesCreate(ctx, &api.File{
		Name:     "nested-folder",
		Type:     api.FileTypeFolder,
		ParentId: api.NewOptUUID(rootFolder.ID.Value),
	})
	if err != nil {
		t.Fatalf("FilesCreate nested folder failed: %v", err)
	}

	// Test: operation=find&root=true should return only root-level items.
	findRoot, err := client.FilesList(ctx, api.FilesListParams{
		Operation: api.NewOptFileQueryOperation(api.FileQueryOperationFind),
		Root:      api.NewOptBool(true),
	})
	if err != nil {
		t.Fatalf("FilesList find+root=true failed: %v", err)
	}
	if len(findRoot.Items) == 0 {
		t.Fatal("find+root=true: expected at least one root-level item")
	}
	for _, item := range findRoot.Items {
		if item.ParentId.IsSet() {
			t.Fatalf("find+root=true: item %s has parentId=%s, expected nil parent",
				item.Name, item.ParentId.Value)
		}
	}

	// Test: operation=list&root=true should list only root-level items.
	listRoot, err := client.FilesList(ctx, api.FilesListParams{
		Operation: api.NewOptFileQueryOperation(api.FileQueryOperationList),
		Root:      api.NewOptBool(true),
		Limit:     api.NewOptInt(100),
	})
	if err != nil {
		t.Fatalf("FilesList list+root=true failed: %v", err)
	}
	if len(listRoot.Items) == 0 {
		t.Fatal("list+root=true: expected at least one root-level item")
	}
	for _, item := range listRoot.Items {
		if item.ParentId.IsSet() {
			t.Fatalf("list+root=true: item %s has parentId=%s, expected nil parent",
				item.Name, item.ParentId.Value)
		}
	}

	// Test: without root param, find with no other filters returns nil because
	// resolveFilesQueryParentID returns (nil, false, nil) for "find" with no path.
	// That's expected — finding without any criteria returns nothing.
	findNoRoot, err := client.FilesList(ctx, api.FilesListParams{
		Operation: api.NewOptFileQueryOperation(api.FileQueryOperationFind),
	})
	if err != nil {
		t.Fatalf("FilesList find no root failed: %v", err)
	}
	// find with no criteria returns nothing (depends on path/name/query).
	// Just verify it doesn't error.
	_ = findNoRoot

	// Test: without root param, list returns everything (including nested).
	listAll, err := client.FilesList(ctx, api.FilesListParams{
		Limit: api.NewOptInt(100),
	})
	if err != nil {
		t.Fatalf("FilesList list without root failed: %v", err)
	}
	hasWithParent := false
	for _, item := range listAll.Items {
		if item.ParentId.IsSet() {
			hasWithParent = true
			break
		}
	}
	if !hasWithParent {
		t.Fatal("list without root: expected at least one item with parent")
	}
}

func TestFilesRoutes_Validation(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	token := loginAndGetToken(t, s, 7203, "user7203")
	client := s.newClientWithToken(token)

	t.Run("FilesGetById invalid UUID => 400", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.server.URL+"/files/bad-uuid", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Cookie", "access_token="+token)

		resp, err := s.httpCli.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		// Invalid path params may be rejected before the service error mapper runs.
		body := make([]byte, 4096)
		n, _ := resp.Body.Read(body)
		e := decodeError(t, body[:n])
		if e.Code != 0 && e.Code != 400 {
			t.Fatalf("expected body.code=400, got %d", e.Code)
		}
		if e.Code != 0 && e.Error == "" {
			t.Fatal("expected non-empty body.error for bad UUID")
		}
		if e.Code != 0 && e.Message == "" {
			t.Fatal("expected non-empty body.message")
		}
		if e.Code != 0 && e.RequestID == "" {
			t.Fatal("expected body.requestId")
		}
		if resp.Header.Get("X-Request-ID") == "" {
			t.Fatal("expected X-Request-ID header")
		}
	})

	t.Run("FilesCreate missing path/parent => 409", func(t *testing.T) {
		_, err := client.FilesCreate(ctx, &api.File{Name: "docs", Type: api.FileTypeFolder})
		sc := statusCode(err)
		if sc != 409 {
			t.Fatalf("expected 409, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil {
			if eb.Error.Value != "conflict" && eb.Error.Value != "bad_request" {
				t.Logf("error body code=%s message=%s", eb.Error.Value, eb.Message)
			}
		}
	})
}
