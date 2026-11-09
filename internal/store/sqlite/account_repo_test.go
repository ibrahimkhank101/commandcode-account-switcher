package sqlite_test

import (
	"context"
	"testing"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/store/sqlite"
)

func TestAccountRepoCRUD(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	repo := sqlite.NewAccountRepo(db)
	ctx := context.Background()

	acc := &domain.Account{
		Alias:    "test-acc",
		APIKey:   "user_test123",
		UserID:   "uid-1",
		UserName: "testuser",
		Email:    "test@example.com",
		PlanID:   "individual-go-v1",
		KeyName:  "cli-test",
		IsActive: true,
		Status:   domain.StatusActive,
		Priority: 0,
		Quota: domain.AccountQuota{
			FiveHour: domain.WindowLimit{Used: 1.0, Cap: 3.0},
		},
	}

	if err := repo.Create(ctx, acc); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found, err := repo.GetByAlias(ctx, "test-acc")
	if err != nil {
		t.Fatalf("GetByAlias failed: %v", err)
	}
	if found.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", found.Email)
	}

	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("GetActive failed: %v", err)
	}
	if active.ID != acc.ID {
		t.Errorf("expected active id %s, got %s", acc.ID, active.ID)
	}

	// Update quota
	newQuota := domain.AccountQuota{
		FiveHour: domain.WindowLimit{Used: 2.5, Cap: 3.0},
	}
	if err := repo.UpdateQuota(ctx, acc.ID, newQuota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	updated, _ := repo.GetByID(ctx, acc.ID)
	if updated.Quota.FiveHour.Used != 2.5 {
		t.Errorf("expected updated quota 2.5, got %f", updated.Quota.FiveHour.Used)
	}

	// Delete
	if err := repo.Delete(ctx, acc.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = repo.GetByID(ctx, acc.ID)
	if err != domain.ErrAccountNotFound {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}
