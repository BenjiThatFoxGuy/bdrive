package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func newPeriodicJobRepo(s *harness) *repositories.JetPeriodicJobRepository {
	return repositories.NewJetPeriodicJobRepository(s.pool)
}

func TestPeriodicJobCreateAndListByUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8201)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()

	job1 := &repositories.PeriodicJob{
		ID:             uuid.New(),
		UserID:         uid,
		Name:           "test-job-1",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	job2 := &repositories.PeriodicJob{
		ID:             uuid.New(),
		UserID:         uid,
		Name:           "test-job-2",
		Kind:           "clean.stale_uploads",
		Args:           repositories.CleanStaleUploadsPeriodicArgs{Retention: "1h"},
		CronExpression: "30 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job1); err != nil {
		t.Fatalf("Create job1 failed: %v", err)
	}
	if err := repo.Create(ctx, job2); err != nil {
		t.Fatalf("Create job2 failed: %v", err)
	}

	jobs, err := repo.ListByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("ListByUserID failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	// Verify both job names are present
	names := map[string]bool{}
	for _, j := range jobs {
		names[j.Name] = true
	}
	if !names["test-job-1"] {
		t.Error("expected job 'test-job-1' in list")
	}
	if !names["test-job-2"] {
		t.Error("expected job 'test-job-2' in list")
	}
}

func TestPeriodicJobListEnabled(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8202)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()

	enabledJob := &repositories.PeriodicJob{
		ID:             uuid.New(),
		UserID:         uid,
		Name:           "enabled-job",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	disabledJob := &repositories.PeriodicJob{
		ID:             uuid.New(),
		UserID:         uid,
		Name:           "disabled-job",
		Kind:           "clean.stale_uploads",
		Args:           repositories.CleanStaleUploadsPeriodicArgs{Retention: "1h"},
		CronExpression: "30 * * * *",
		Enabled:        false,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, enabledJob); err != nil {
		t.Fatalf("Create enabled job failed: %v", err)
	}
	if err := repo.Create(ctx, disabledJob); err != nil {
		t.Fatalf("Create disabled job failed: %v", err)
	}

	jobs, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}

	// Should only contain the enabled job
	for _, j := range jobs {
		if j.Name == "disabled-job" {
			t.Errorf("ListEnabled returned disabled job: %s", j.Name)
		}
	}

	// Verify the enabled job is present
	found := false
	for _, j := range jobs {
		if j.Name == "enabled-job" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListEnabled did not return the enabled job")
	}
}

func TestPeriodicJobGetByIDAndUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8203)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()
	jobID := uuid.New()

	job := &repositories.PeriodicJob{
		ID:             jobID,
		UserID:         uid,
		Name:           "get-by-id-job",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByIDAndUserID(ctx, jobID, uid)
	if err != nil {
		t.Fatalf("GetByIDAndUserID failed: %v", err)
	}
	if got.ID != jobID {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, jobID)
	}
	if got.Name != "get-by-id-job" {
		t.Errorf("Name mismatch: got %s, want get-by-id-job", got.Name)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
	if !got.Enabled {
		t.Error("Expected Enabled to be true")
	}
}

func TestPeriodicJobGetByIDAndUserID_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	repo := newPeriodicJobRepo(s)

	_, err := repo.GetByIDAndUserID(ctx, uuid.New(), 9999)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPeriodicJobGetByNameAndUserID(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8205)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()

	job := &repositories.PeriodicJob{
		ID:             uuid.New(),
		UserID:         uid,
		Name:           "get-by-name-job",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := repo.GetByNameAndUserID(ctx, uid, "get-by-name-job")
	if err != nil {
		t.Fatalf("GetByNameAndUserID failed: %v", err)
	}
	if got.Name != "get-by-name-job" {
		t.Errorf("Name mismatch: got %s, want get-by-name-job", got.Name)
	}
	if got.UserID != uid {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, uid)
	}
}

func TestPeriodicJobUpdate(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8206)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()
	jobID := uuid.New()

	job := &repositories.PeriodicJob{
		ID:             jobID,
		UserID:         uid,
		Name:           "old-name",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	updatedNow := time.Now().UTC()
	updatedJob := repositories.PeriodicJob{
		Name:           "new-name",
		CronExpression: "30 * * * *",
		Enabled:        true,
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "48h"},
		UpdatedAt:      updatedNow,
	}

	if err := repo.Update(ctx, jobID, uid, updatedJob); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := repo.GetByIDAndUserID(ctx, jobID, uid)
	if err != nil {
		t.Fatalf("GetByIDAndUserID after update failed: %v", err)
	}
	if got.Name != "new-name" {
		t.Errorf("Name not updated: got %s, want new-name", got.Name)
	}
	if got.CronExpression != "30 * * * *" {
		t.Errorf("CronExpression not updated: got %s, want 30 * * * *", got.CronExpression)
	}
}

func TestPeriodicJobDelete(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8207)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()
	jobID := uuid.New()

	job := &repositories.PeriodicJob{
		ID:             jobID,
		UserID:         uid,
		Name:           "delete-job",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        true,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify exists
	if _, err := repo.GetByIDAndUserID(ctx, jobID, uid); err != nil {
		t.Fatalf("GetByIDAndUserID before delete failed: %v", err)
	}

	// Delete
	if err := repo.Delete(ctx, jobID, uid); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify gone
	_, err := repo.GetByIDAndUserID(ctx, jobID, uid)
	if err != repositories.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestPeriodicJobSetEnabled(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()
	uid := int64(8208)
	s.ensureUserExists(uid)

	repo := newPeriodicJobRepo(s)
	now := time.Now().UTC()
	jobID := uuid.New()

	job := &repositories.PeriodicJob{
		ID:             jobID,
		UserID:         uid,
		Name:           "set-enabled-job",
		Kind:           "clean.old_events",
		Args:           repositories.CleanOldEventsPeriodicArgs{Retention: "24h"},
		CronExpression: "0 * * * *",
		Enabled:        false,
		System:         false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repo.Create(ctx, job); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Enable the job
	if err := repo.SetEnabled(ctx, jobID, uid, true, time.Now().UTC()); err != nil {
		t.Fatalf("SetEnabled failed: %v", err)
	}

	// Verify it appears in ListEnabled
	jobs, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled failed: %v", err)
	}

	found := false
	for _, j := range jobs {
		if j.ID == jobID {
			found = true
			if !j.Enabled {
				t.Error("job should be enabled")
			}
			break
		}
	}
	if !found {
		t.Error("job not found in ListEnabled after being enabled")
	}
}
