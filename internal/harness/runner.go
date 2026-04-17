package harness

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// defaultRunner is the production implementation of runCmd.
// It executes the named command with the given args in dir (or the current
// directory when dir is ""), with a 60-second timeout.
func defaultRunner(dir, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w\noutput: %s", name, args, err, string(out))
	}
	return nil
}
