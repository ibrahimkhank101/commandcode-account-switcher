package launcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

func FindCommandCodeExecutable() (string, []string, error) {
	names := []string{"command-code", "cmdc"}
	if runtime.GOOS == "windows" {
		names = []string{"command-code.cmd", "command-code.ps1", "cmdc.cmd", "cmdc.ps1", "command-code", "cmdc"}
	}

	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			if filepath.Ext(path) == ".ps1" {
				return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path}, nil
			}
			return path, nil, nil
		}
	}

	home, _ := os.UserHomeDir()
	wingetNode := filepath.Join(home, "AppData", "Local", "Microsoft", "WinGet", "Packages")
	matches, _ := filepath.Glob(filepath.Join(wingetNode, "*NodeJS*", "*win-x64", "command-code.ps1"))
	if len(matches) > 0 {
		return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", matches[0]}, nil
	}

	return "", nil, fmt.Errorf("command-code executable not found in PATH")
}

func RunSupervised(ctx context.Context, handler http.Handler, port int, userArgs []string) int {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: proxy failed to bind to %s: %v\n", addr, err)
		return 1
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	fmt.Printf("[Command Code Switcher] Proxy supervisor listening on http://%s\n", addr)
	fmt.Printf("[Command Code Switcher] Upstream API routed transparently to https://api.commandcode.ai\n")

	exe, prefixArgs, err := FindCommandCodeExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		_ = server.Shutdown(context.Background())
		return 1
	}

	fullArgs := append(prefixArgs, userArgs...)
	cmd := exec.CommandContext(ctx, exe, fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd.Env = append(os.Environ(),
		"COMMANDCODE_SANDBOX=true",
		"COMMANDCODE_API_URL="+proxyURL,
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	cmdErr := cmd.Run()
	signal.Stop(sigCh)
	close(sigCh)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)

	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
