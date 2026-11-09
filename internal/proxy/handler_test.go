package proxy_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/config"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/proxy"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/store/sqlite"
)

func TestFailoverReplayOn429(t *testing.T) {
	// Setup test database
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	repo := sqlite.NewAccountRepo(db)
	eventRepo := sqlite.NewEventRepo(db)
	broadcaster := domain.NewInMemoryBroadcaster()

	ctx := context.Background()

	// Seed Account 1 (will 429)
	acc1 := &domain.Account{
		Alias:    "acc1",
		APIKey:   "key-1",
		UserID:   "user-1",
		UserName: "acc1",
		Email:    "acc1@example.com",
		IsActive: true,
		Status:   domain.StatusActive,
		Quota: domain.AccountQuota{
			FiveHour: domain.WindowLimit{Used: 2.9, Cap: 3.0},
		},
		Priority: 0,
	}
	_ = repo.Create(ctx, acc1)

	// Seed Account 2 (will succeed)
	acc2 := &domain.Account{
		Alias:    "acc2",
		APIKey:   "key-2",
		UserID:   "user-2",
		UserName: "acc2",
		Email:    "acc2@example.com",
		IsActive: false,
		Status:   domain.StatusActive,
		Quota: domain.AccountQuota{
			FiveHour: domain.WindowLimit{Used: 0.1, Cap: 3.0},
		},
		Priority: 1,
	}
	_ = repo.Create(ctx, acc2)

	// Upstream mock server
	var attempts int32
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		authHeader := r.Header.Get("Authorization")

		if att == 1 {
			// First attempt should be key-1
			if authHeader != "Bearer key-1" {
				t.Errorf("expected first attempt to use key-1, got %s", authHeader)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Window limits exceeded","exceeded":"fiveHour"}`))
			return
		}

		// Second attempt should failover to key-2
		if authHeader != "Bearer key-2" {
			t.Errorf("expected failover attempt to use key-2, got %s", authHeader)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"message":"Replayed successfully"}`))
	}))
	defer mockUpstream.Close()

	cfg := &config.Config{
		TargetURL:    mockUpstream.URL,
		MaxRetries:   3,
		MaxBodyBytes: 1024 * 1024,
	}

	fe := proxy.NewFailoverEngine(repo, eventRepo, broadcaster, nil)
	handler, err := proxy.NewProxyHandler(cfg, repo, fe, broadcaster)
	if err != nil {
		t.Fatalf("failed to create proxy handler: %v", err)
	}

	// Dispatch request to proxy handler
	req := httptest.NewRequest(http.MethodPost, "/alpha/chat", strings.NewReader(`{"prompt":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 after failover replay, got %d. Body: %s", resp.StatusCode, string(body))
	}

	if !strings.Contains(string(body), "Replayed successfully") {
		t.Fatalf("expected replayed response body, got: %s", string(body))
	}

	// Verify Account 1 is now marked exhausted and Account 2 is active
	active, err := repo.GetActive(ctx)
	if err != nil {
		t.Fatalf("failed to get active account: %v", err)
	}
	if active.Alias != "acc2" {
		t.Fatalf("expected active account to be acc2, got %s", active.Alias)
	}

	acc1Updated, _ := repo.GetByAlias(ctx, "acc1")
	if acc1Updated.Status != domain.StatusExhausted {
		t.Fatalf("expected acc1 to be StatusExhausted, got %s", acc1Updated.Status)
	}
}
