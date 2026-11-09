package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func RunWrap(ctx context.Context, cmdArgs []string, port int) int {
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cmd-switch wrap -- <command> [args...]\n")
		return 1
	}

	exe := cmdArgs[0]
	args := cmdArgs[1:]

	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	proxyURL := fmt.Sprintf("http://127.0.0.1:%s", strconv.Itoa(port))
	cmd.Env = append(os.Environ(),
		"COMMANDCODE_SANDBOX=true",
		"COMMANDCODE_API_URL="+proxyURL,
	)

	if err := cmd.Run(); err != nil {
		if exitErr, ok := cmdErr(err); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Failed to run command: %v\n", err)
		return 1
	}
	return 0
}

func cmdErr(err error) (*exec.ExitError, bool) {
	exitErr, ok := err.(*exec.ExitError)
	return exitErr, ok
}
