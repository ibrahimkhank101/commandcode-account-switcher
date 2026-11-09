package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/config"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
)

type ProxyHandler struct {
	cfg            *config.Config
	accountRepo    domain.AccountRepository
	failoverEngine *FailoverEngine
	targetURL      *url.URL
	httpClient     *http.Client
	broadcaster    domain.EventBroadcaster
}

func NewProxyHandler(
	cfg *config.Config,
	repo domain.AccountRepository,
	fe *FailoverEngine,
	broadcaster domain.EventBroadcaster,
) (*ProxyHandler, error) {
	u, err := url.Parse(cfg.TargetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream target url: %w", err)
	}

	return &ProxyHandler{
		cfg:            cfg,
		accountRepo:    repo,
		failoverEngine: fe,
		targetURL:      u,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		broadcaster: broadcaster,
	}, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	buffered, err := NewBufferedRequestWithLimit(r, h.cfg.MaxBodyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to buffer request body: %v", err), http.StatusBadRequest)
		return
	}

	currentAcc, err := h.accountRepo.GetActive(ctx)
	if err != nil {
		currentAcc, err = h.accountRepo.GetNextAvailable(ctx, "")
		if err != nil {
			http.Error(w, "no accounts configured in pool. Add one using 'cmd-switch add-account'", http.StatusServiceUnavailable)
			return
		}
		_ = h.accountRepo.SetActive(ctx, currentAcc.ID)
	}

	maxAttempts := h.cfg.MaxRetries
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	for attempt := 0; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}

		destURL := *h.targetURL
		destURL.Path = r.URL.Path
		destURL.RawQuery = r.URL.RawQuery

		outReq, err := http.NewRequestWithContext(ctx, r.Method, destURL.String(), nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to create upstream request: %v", err), http.StatusInternalServerError)
			return
		}

		for k, vv := range r.Header {
			for _, v := range vv {
				outReq.Header.Add(k, v)
			}
		}

		outReq.Header.Set("Authorization", "Bearer "+currentAcc.APIKey)
		outReq.Host = h.targetURL.Host

		buffered.ResetBody(outReq)

		resp, err := h.httpClient.Do(outReq)
		if err != nil {
			if attempt < maxAttempts {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			http.Error(w, fmt.Sprintf("upstream gateway error: %v", err), http.StatusBadGateway)
			return
		}

		if IsExhaustionResponse(resp.StatusCode, nil) {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if IsExhaustionResponse(resp.StatusCode, respBody) && attempt < maxAttempts {
				reason := fmt.Sprintf("status %d", resp.StatusCode)
				if len(respBody) > 0 {
					reason = fmt.Sprintf("status %d: %s", resp.StatusCode, string(bytes.TrimSpace(respBody)))
				}

				nextAcc, rotErr := h.failoverEngine.HandleExhaustion(ctx, currentAcc, reason)
				if rotErr == nil && nextAcc != nil {
					currentAcc = nextAcc
					continue
				}
			}

			for k, vv := range resp.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(respBody)
			return
		}

		_ = h.accountRepo.RecordUsage(ctx, currentAcc.ID, attempt > 0)

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)

		flusher, isFlusher := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, rErr := resp.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
				if isFlusher {
					flusher.Flush()
				}
			}
			if rErr != nil {
				break
			}
		}
		resp.Body.Close()
		return
	}
}
