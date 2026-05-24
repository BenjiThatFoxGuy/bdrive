package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-faster/jx"
	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/internal/api"
)

func TestPeriodicJobsRoutes_MaintenanceJobs(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7311, "user7311")

	list, err := client.PeriodicJobsList(ctx)
	if err != nil {
		t.Fatalf("PeriodicJobsList failed: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected default maintenance jobs, got %d", len(list))
	}

	foundKinds := map[string]bool{}
	for _, item := range list {
		foundKinds[string(item.Kind)] = true
	}
	if !foundKinds["clean.old_events"] || !foundKinds["clean.pending_files"] {
		t.Fatalf("expected maintenance jobs in list, got %+v", list)
	}

	assertMaintenanceRetention := func(kind api.PeriodicJobKind, expected string) {
		for _, item := range list {
			if item.Kind == kind {
				detail, err := client.PeriodicJobsGet(ctx, api.PeriodicJobsGetParams{ID: item.ID})
				if err != nil {
					t.Fatalf("PeriodicJobsGet(%s) failed: %v", kind, err)
				}
				args, ok := detail.Args.Get()
				if !ok {
					t.Fatalf("expected args for %s", kind)
				}
				retentionRaw, ok := args["retention"]
				if !ok {
					t.Fatalf("expected retention in args for %s: %+v", kind, args)
				}
				var retention string
				if err := json.Unmarshal(retentionRaw, &retention); err != nil {
					t.Fatalf("unmarshal retention for %s: %v", kind, err)
				}
				if retention != expected {
					t.Fatalf("expected retention=%s for %s, got %s", expected, kind, retention)
				}
			}
		}
	}

	assertMaintenanceRetention(api.PeriodicJobKindCleanOldEvents, "5d")
	assertMaintenanceRetention(api.PeriodicJobKindCleanStaleUploads, "1d")
}

func TestPeriodicJobsRoutes_SystemJobProtection(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	_, client, _ := loginWithClient(t, s, 7312, "user7312")

	items, err := client.PeriodicJobsList(ctx)
	if err != nil {
		t.Fatalf("PeriodicJobsList failed: %v", err)
	}

	var maintenanceID api.UUID
	for _, item := range items {
		maintenanceID = item.ID
		break
	}
	if uuid.UUID(maintenanceID) == uuid.Nil {
		t.Fatalf("expected a maintenance job")
	}

	t.Run("system jobs cannot be deleted", func(t *testing.T) {
		err := client.PeriodicJobsDelete(ctx, api.PeriodicJobsDeleteParams{ID: maintenanceID})
		sc := statusCode(err)
		if sc != 400 {
			t.Fatalf("expected 400, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 400 {
			t.Fatalf("expected body.code=400, got %d", eb.Code)
		}
	})

	t.Run("maintenance retention can be updated", func(t *testing.T) {
		items, err := client.PeriodicJobsList(ctx)
		if err != nil {
			t.Fatalf("PeriodicJobsList failed: %v", err)
		}
		var staleUploadsID api.UUID
		for _, item := range items {
			if item.Kind == api.PeriodicJobKindCleanStaleUploads {
				staleUploadsID = item.ID
				break
			}
		}
		if uuid.UUID(staleUploadsID) == uuid.Nil {
			t.Fatalf("expected clean.stale_uploads maintenance job")
		}

		updated, err := client.PeriodicJobsUpdate(ctx, &api.PeriodicJobUpdate{
			Args: api.NewOptPeriodicJobUpdateArgs(api.PeriodicJobUpdateArgs{"retention": jx.Raw(`"2h"`)}),
		}, api.PeriodicJobsUpdateParams{ID: staleUploadsID})
		if err != nil {
			t.Fatalf("PeriodicJobsUpdate failed: %v", err)
		}
		args, ok := updated.Args.Get()
		if !ok {
			t.Fatalf("expected updated args")
		}
		retentionRaw, ok := args["retention"]
		if !ok {
			t.Fatalf("expected retention in args: %+v", args)
		}
		var retention string
		if err := json.Unmarshal(retentionRaw, &retention); err != nil {
			t.Fatalf("unmarshal retention: %v", err)
		}
		if retention != "2h0m0s" {
			t.Fatalf("expected retention=2h0m0s, got %s", retention)
		}
	})

	t.Run("pending files args update is ignored", func(t *testing.T) {
		items, err := client.PeriodicJobsList(ctx)
		if err != nil {
			t.Fatalf("PeriodicJobsList failed: %v", err)
		}
		var pendingFilesID api.UUID
		for _, item := range items {
			if item.Kind == api.PeriodicJobKindCleanPendingFiles {
				pendingFilesID = item.ID
				break
			}
		}
		if uuid.UUID(pendingFilesID) == uuid.Nil {
			t.Fatalf("expected clean.pending_files maintenance job")
		}

		updated, err := client.PeriodicJobsUpdate(ctx, &api.PeriodicJobUpdate{
			Args: api.NewOptPeriodicJobUpdateArgs(api.PeriodicJobUpdateArgs{"retention": jx.Raw(`"1h"`)}),
		}, api.PeriodicJobsUpdateParams{ID: pendingFilesID})
		if err != nil {
			t.Fatalf("PeriodicJobsUpdate failed: %v", err)
		}
		if args, ok := updated.Args.Get(); ok {
			if _, exists := args["retention"]; exists {
				t.Fatalf("expected retention arg to be ignored, got %+v", args)
			}
		}
	})

	t.Run("refresh folder sizes args update is ignored", func(t *testing.T) {
		items, err := client.PeriodicJobsList(ctx)
		if err != nil {
			t.Fatalf("PeriodicJobsList failed: %v", err)
		}
		var refreshID api.UUID
		for _, item := range items {
			if item.Kind == api.PeriodicJobKind("refresh.folder_sizes") {
				refreshID = item.ID
				break
			}
		}
		if uuid.UUID(refreshID) == uuid.Nil {
			t.Fatalf("expected refresh.folder_sizes maintenance job")
		}

		updated, err := client.PeriodicJobsUpdate(ctx, &api.PeriodicJobUpdate{
			Args: api.NewOptPeriodicJobUpdateArgs(api.PeriodicJobUpdateArgs{"retention": jx.Raw(`"1h"`)}),
		}, api.PeriodicJobsUpdateParams{ID: refreshID})
		if err != nil {
			t.Fatalf("PeriodicJobsUpdate failed: %v", err)
		}
		if args, ok := updated.Args.Get(); ok {
			if _, exists := args["retention"]; exists {
				t.Fatalf("expected retention arg to be ignored, got %+v", args)
			}
		}
	})

	t.Run("run unknown periodic job returns 404", func(t *testing.T) {
		_, err := client.PeriodicJobsRun(ctx, api.PeriodicJobsRunParams{ID: api.UUID(uuid.MustParse("00000000-0000-0000-0000-000000000000"))})
		sc := statusCode(err)
		if sc != 404 {
			t.Fatalf("expected 404, got %d err=%v", sc, err)
		}
		if eb := errorResponse(err); eb != nil && eb.Code != 404 {
			t.Fatalf("expected body.code=404, got %d", eb.Code)
		}
	})
}
