package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/api"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/vault"
)

//go:embed dist/*
var distFS embed.FS

type Server struct {
	repo         domain.AccountRepository
	broadcaster  domain.EventBroadcaster
	apiClient    *api.Client
	vaultManager *vault.Manager
}

func NewServer(
	repo domain.AccountRepository,
	broadcaster domain.EventBroadcaster,
	apiClient *api.Client,
	vManager *vault.Manager,
) *Server {
	return &Server{
		repo:         repo,
		broadcaster:  broadcaster,
		apiClient:    apiClient,
		vaultManager: vManager,
	}
}

func (s *Server) Routes(proxyHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	dist, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(dist))

	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/accounts/switch", s.handleSwitch)
	mux.HandleFunc("/api/accounts/refresh", s.handleRefresh)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/accounts/", s.handleAccountDelete)
	mux.HandleFunc("/api/events", s.handleEvents)

	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/alpha") || r.Header.Get("User-Agent") == "command-code" {
			proxyHandler.ServeHTTP(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts, err := s.repo.List(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var totalReqs, totalFailovers int64
	for _, a := range accounts {
		totalReqs += a.RequestCount
		totalFailovers += a.FailoverCount
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"accounts":       accounts,
		"totalRequests":  totalReqs,
		"totalFailovers": totalFailovers,
	})
}

func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		ID    string `json:"id"`
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var acc *domain.Account
	var err error

	if payload.ID != "" {
		acc, err = s.repo.GetByID(ctx, payload.ID)
	} else if payload.Alias != "" {
		acc, err = s.repo.GetByAlias(ctx, payload.Alias)
	}
	if err != nil || acc == nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	if err := s.repo.SetActive(ctx, acc.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if s.vaultManager != nil {
		_ = s.vaultManager.AtomicWriteAuth(&vault.AuthFile{
			APIKey:          acc.APIKey,
			UserID:          acc.UserID,
			UserName:        acc.UserName,
			KeyName:         acc.KeyName,
			AuthenticatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}

	if s.broadcaster != nil {
		s.broadcaster.Publish(domain.Event{
			Type:         domain.EventAccountSwitched,
			AccountID:    acc.ID,
			AccountAlias: acc.Alias,
			Message:      fmt.Sprintf("Active account switched to %s", acc.Alias),
			Timestamp:    time.Now().UTC(),
		})
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accounts, err := s.repo.List(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, acc := range accounts {
		quota, err := s.apiClient.FetchCredits(ctx, acc.APIKey)
		if err == nil && quota != nil {
			_ = s.repo.UpdateQuota(ctx, acc.ID, *quota)
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.handleStatus(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Alias  string `json:"alias"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if payload.APIKey == "" && s.vaultManager != nil {
		auth, err := s.vaultManager.ReadCurrentAuth()
		if err != nil {
			http.Error(w, "failed to read active auth.json: "+err.Error(), http.StatusBadRequest)
			return
		}
		payload.APIKey = auth.APIKey
	}

	whoami, err := s.apiClient.FetchWhoami(ctx, payload.APIKey)
	if err != nil {
		http.Error(w, "invalid API key: "+err.Error(), http.StatusBadRequest)
		return
	}

	alias := payload.Alias
	if alias == "" {
		alias = whoami.User.UserName
	}

	quota, _ := s.apiClient.FetchCredits(ctx, payload.APIKey)
	sub, _ := s.apiClient.FetchSubscription(ctx, payload.APIKey)

	planID := "free"
	if sub != nil && sub.Data.PlanID != "" {
		planID = sub.Data.PlanID
	}

	var q domain.AccountQuota
	if quota != nil {
		q = *quota
	}

	acc := &domain.Account{
		Alias:    alias,
		APIKey:   payload.APIKey,
		UserID:   whoami.User.ID,
		UserName: whoami.User.UserName,
		Email:    whoami.User.Email,
		PlanID:   planID,
		KeyName:  "custom-" + alias,
		Status:   domain.StatusActive,
		Quota:    q,
	}

	if err := s.repo.Create(ctx, acc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := s.repo.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch, unsubscribe := s.broadcaster.Subscribe()
	defer unsubscribe()

	_, _ = fmt.Fprintf(w, "data: %s\n\n", "{\"type\":\"system\",\"message\":\"Connected to telemetry feed\"}")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			data, err := json.Marshal(event)
			if err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(data))
				flusher.Flush()
			}
		}
	}
}
