package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/api"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/config"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/domain"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/launcher"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/proxy"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/quota"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/store/sqlite"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/vault"
	"github.com/ibrahimkhank101/commandcode-account-switcher/internal/web"
)

var (
	Version = "1.0.0"
	Commit  = "beambeprep-v1"
	Date    = "2026-11-09"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.DefaultConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	accountRepo := sqlite.NewAccountRepo(db)
	eventRepo := sqlite.NewEventRepo(db)
	broadcaster := domain.NewInMemoryBroadcaster()
	apiClient := api.NewClient(cfg.TargetURL)
	vaultManager := vault.NewManager(cfg, accountRepo, apiClient)
	failoverEngine := proxy.NewFailoverEngine(accountRepo, eventRepo, broadcaster, vaultManager)

	// Auto-import active login if pool is empty
	ctx := context.Background()
	if cfg.AutoImportOnStart {
		accounts, _ := accountRepo.List(ctx)
		if len(accounts) == 0 {
			_, _ = vaultManager.AutoImportActive(ctx)
		}
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "launch":
		runLaunch(ctx, cfg, accountRepo, failoverEngine, broadcaster, apiClient, vaultManager, args)
	case "serve":
		runServe(ctx, cfg, accountRepo, failoverEngine, broadcaster, apiClient, vaultManager, args)
	case "wrap":
		runWrap(ctx, cfg, args)
	case "add-account":
		runAddAccount(ctx, accountRepo, apiClient, vaultManager, args)
	case "list-accounts", "list", "ls":
		runListAccounts(ctx, accountRepo)
	case "switch", "use":
		runSwitch(ctx, accountRepo, vaultManager, args)
	case "status":
		runStatus(ctx, accountRepo, cfg)
	case "refresh-quotas", "refresh":
		runRefreshQuotas(ctx, accountRepo, apiClient)
	case "remove-account", "rm":
		runRemoveAccount(ctx, accountRepo, args)
	case "version", "-v", "--version":
		fmt.Printf("Command Code Switcher v%s (%s, %s)\n", Version, Commit, Date)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`Command Code Switcher v%s
Autonomous account rotation, multi-account pool manager, and transparent proxy supervisor for Command Code.

Usage:
  cmd-switch <command> [arguments]

Commands:
  launch [args...]       Launch Command Code with transparent proxy supervision and automatic 429 rotation
  serve                  Run supervisor proxy daemon and web dashboard on http://127.0.0.1:8766
  wrap -- <command...>   Run any command with scoped Command Code proxy environment variables
  add-account            Onboard an account into the rotation pool
  list-accounts          Display all accounts, status, 5-hour quota usage, and active indicators
  switch <alias>         Atomically switch active account in ~/.commandcode/auth.json and pool
  refresh-quotas         Fetch live credits and rate limit telemetry for all pool accounts
  status                 Display active account, pool readiness, and supervisor health
  remove-account <alias> Remove an account from the pool
  version                Display version information
`, Version)
}

func runLaunch(
	ctx context.Context,
	cfg *config.Config,
	repo domain.AccountRepository,
	fe *proxy.FailoverEngine,
	broadcaster domain.EventBroadcaster,
	client *api.Client,
	vManager *vault.Manager,
	args []string,
) {
	proxyHandler, err := proxy.NewProxyHandler(cfg, repo, fe, broadcaster)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize proxy handler: %v\n", err)
		os.Exit(1)
	}

	webServer := web.NewServer(repo, broadcaster, client, vManager)
	combinedHandler := webServer.Routes(proxyHandler)

	poller := quota.NewPoller(repo, client, broadcaster, cfg.PollInterval)
	pollerCtx, cancelPoller := context.WithCancel(ctx)
	defer cancelPoller()
	go poller.Start(pollerCtx)

	code := launcher.RunSupervised(ctx, combinedHandler, cfg.Port, args)
	os.Exit(code)
}

func runServe(
	ctx context.Context,
	cfg *config.Config,
	repo domain.AccountRepository,
	fe *proxy.FailoverEngine,
	broadcaster domain.EventBroadcaster,
	client *api.Client,
	vManager *vault.Manager,
	args []string,
) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", cfg.Port, "Port to listen on")
	_ = fs.Parse(args)

	proxyHandler, err := proxy.NewProxyHandler(cfg, repo, fe, broadcaster)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize proxy handler: %v\n", err)
		os.Exit(1)
	}

	webServer := web.NewServer(repo, broadcaster, client, vManager)
	combinedHandler := webServer.Routes(proxyHandler)

	poller := quota.NewPoller(repo, client, broadcaster, cfg.PollInterval)
	pollerCtx, cancelPoller := context.WithCancel(ctx)
	defer cancelPoller()
	go poller.Start(pollerCtx)

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", *port),
		Handler: combinedHandler,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\n[Command Code Switcher] Shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Printf("[Command Code Switcher] Supervisor daemon running on http://127.0.0.1:%d\n", *port)
	fmt.Printf("[Command Code Switcher] Dashboard available at http://127.0.0.1:%d\n", *port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func runWrap(ctx context.Context, cfg *config.Config, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cmd-switch wrap -- <command> [args...]\n")
		os.Exit(1)
	}
	var cmdArgs []string
	if args[0] == "--" {
		cmdArgs = args[1:]
	} else {
		cmdArgs = args
	}
	code := launcher.RunWrap(ctx, cmdArgs, cfg.Port)
	os.Exit(code)
}

func runAddAccount(
	ctx context.Context,
	repo domain.AccountRepository,
	client *api.Client,
	vManager *vault.Manager,
	args []string,
) {
	fs := flag.NewFlagSet("add-account", flag.ExitOnError)
	alias := fs.String("alias", "", "Friendly account alias")
	apiKey := fs.String("key", "", "API key (user_...)")
	_ = fs.Parse(args)

	key := *apiKey
	if key == "" {
		auth, err := vManager.ReadCurrentAuth()
		if err != nil {
			fmt.Fprintf(os.Stderr, "No --key provided and failed to read ~/.commandcode/auth.json: %v\n", err)
			os.Exit(1)
		}
		key = auth.APIKey
		fmt.Printf("Importing key from ~/.commandcode/auth.json (%s)...\n", auth.UserName)
	}

	whoami, err := client.FetchWhoami(ctx, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "API authentication failed: %v\n", err)
		os.Exit(1)
	}

	accountAlias := *alias
	if accountAlias == "" {
		accountAlias = whoami.User.UserName
	}

	quota, _ := client.FetchCredits(ctx, key)
	sub, _ := client.FetchSubscription(ctx, key)

	planID := "free"
	if sub != nil && sub.Data.PlanID != "" {
		planID = sub.Data.PlanID
	}

	var q domain.AccountQuota
	if quota != nil {
		q = *quota
	}

	acc := &domain.Account{
		Alias:    accountAlias,
		APIKey:   key,
		UserID:   whoami.User.ID,
		UserName: whoami.User.UserName,
		Email:    whoami.User.Email,
		PlanID:   planID,
		KeyName:  "custom-" + accountAlias,
		IsActive: true,
		Status:   domain.StatusActive,
		Quota:    q,
	}

	if err := repo.Create(ctx, acc); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save account: %v\n", err)
		os.Exit(1)
	}

	_ = vManager.SaveAccountToVault(accountAlias, &vault.AuthFile{
		APIKey:          key,
		UserID:          whoami.User.ID,
		UserName:        whoami.User.UserName,
		KeyName:         acc.KeyName,
		AuthenticatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	fmt.Printf("Account '%s' (%s) successfully registered in pool.\n", accountAlias, whoami.User.Email)
}

func runListAccounts(ctx context.Context, repo domain.AccountRepository) {
	accounts, err := repo.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list accounts: %v\n", err)
		os.Exit(1)
	}

	if len(accounts) == 0 {
		fmt.Println("No accounts registered. Run 'cmd-switch add-account' to onboard an account.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ACTIVE\tALIAS\tEMAIL\tPLAN\t5-HOUR USAGE\tWEEKLY USAGE\tSTATUS")
	fmt.Fprintln(w, "------\t-----\t-----\t----\t------------\t------------\t------")

	for _, a := range accounts {
		activeMarker := " "
		if a.IsActive {
			activeMarker = "*"
		}

		fiveHourStr := fmt.Sprintf("%.2f/%.2f", a.Quota.FiveHour.Used, a.Quota.FiveHour.Cap)
		weeklyStr := fmt.Sprintf("%.2f/%.2f", a.Quota.Weekly.Used, a.Quota.Weekly.Cap)

		status := string(a.Status)
		if a.Quota.Exceeded != "" {
			status = fmt.Sprintf("exhausted (%s)", a.Quota.Exceeded)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			activeMarker, a.Alias, a.Email, a.PlanID, fiveHourStr, weeklyStr, status,
		)
	}
	w.Flush()
}

func runSwitch(ctx context.Context, repo domain.AccountRepository, vManager *vault.Manager, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cmd-switch switch <alias>\n")
		os.Exit(1)
	}
	alias := args[0]
	acc, err := repo.GetByAlias(ctx, alias)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Account '%s' not found: %v\n", alias, err)
		os.Exit(1)
	}

	if err := repo.SetActive(ctx, acc.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to activate account: %v\n", err)
		os.Exit(1)
	}

	if err := vManager.AtomicWriteAuth(&vault.AuthFile{
		APIKey:          acc.APIKey,
		UserID:          acc.UserID,
		UserName:        acc.UserName,
		KeyName:         acc.KeyName,
		AuthenticatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to update ~/.commandcode/auth.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Switched to '%s' (%s). ~/.commandcode/auth.json updated.\n", acc.Alias, acc.Email)
}

func runStatus(ctx context.Context, repo domain.AccountRepository, cfg *config.Config) {
	acc, err := repo.GetActive(ctx)
	if err != nil {
		fmt.Println("Status: No active account selected.")
		return
	}

	accounts, _ := repo.List(ctx)
	fmt.Printf("Active Account: %s (%s)\n", acc.Alias, acc.Email)
	fmt.Printf("Pool Size:      %d registered account(s)\n", len(accounts))
	fmt.Printf("Plan:           %s\n", acc.PlanID)
	fmt.Printf("5-Hour Quota:   %.2f / %.2f (Reset: %s)\n", acc.Quota.FiveHour.Used, acc.Quota.FiveHour.Cap, acc.Quota.FiveHour.ResetAt.Format(time.Kitchen))
	fmt.Printf("Weekly Quota:   %.2f / %.2f\n", acc.Quota.Weekly.Used, acc.Quota.Weekly.Cap)
	fmt.Printf("Proxy Endpoint: http://127.0.0.1:%d\n", cfg.Port)
}

func runRefreshQuotas(ctx context.Context, repo domain.AccountRepository, client *api.Client) {
	accounts, err := repo.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Refreshing quotas from api.commandcode.ai...")
	for _, a := range accounts {
		quota, err := client.FetchCredits(ctx, a.APIKey)
		if err != nil {
			fmt.Printf("  x %s: failed (%v)\n", a.Alias, err)
			continue
		}
		_ = repo.UpdateQuota(ctx, a.ID, *quota)
		fmt.Printf("  + %s: 5h=%.2f/%.2f weekly=%.2f/%.2f\n", a.Alias, quota.FiveHour.Used, quota.FiveHour.Cap, quota.Weekly.Used, quota.Weekly.Cap)
	}
	fmt.Println("Quotas synchronized.")
}

func runRemoveAccount(ctx context.Context, repo domain.AccountRepository, args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cmd-switch remove-account <alias>\n")
		os.Exit(1)
	}
	alias := args[0]
	acc, err := repo.GetByAlias(ctx, alias)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Account '%s' not found.\n", alias)
		os.Exit(1)
	}
	if err := repo.Delete(ctx, acc.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete account: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Account '%s' removed from pool.\n", alias)
}
