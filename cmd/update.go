package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/1broseidon/cymbal/internal/updatecheck"
	"github.com/spf13/cobra"
)

type updateNoticeContextKey struct{}

var interactiveTerminalFn = isInteractiveTerminal

func currentVersion() string {
	v, _, _ := versionInfo()
	return v
}

func prepareUpdateNotice(cmd *cobra.Command) error {
	if shouldSkipPassiveUpdateNotice(cmd) {
		return nil
	}
	status, err := updatecheck.GetStatus(cmd.Context(), updatecheck.Options{
		CurrentVersion: currentVersion(),
		AllowNetwork:   true,
		Timeout:        400 * time.Millisecond,
	})
	if err != nil || !status.Available || !updatecheck.ShouldNotify(status) {
		return nil
	}
	baseCtx := cmd.Context()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx := context.WithValue(baseCtx, updateNoticeContextKey{}, status)
	cmd.SetContext(ctx)
	return nil
}

func emitUpdateNotice(cmd *cobra.Command) {
	if cmd.Context() == nil {
		return
	}
	v := cmd.Context().Value(updateNoticeContextKey{})
	status, ok := v.(updatecheck.Status)
	if !ok {
		return
	}
	msg := updatecheck.FormatNotice(status)
	if msg == "" {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), msg)
	_ = updatecheck.MarkNotified(status)
}

func shouldSkipPassiveUpdateNotice(cmd *cobra.Command) bool {
	if cmd == nil || updatecheck.Disabled() || !interactiveTerminalFn(os.Stderr) {
		return true
	}
	if strings.HasPrefix(cmd.CommandPath(), "cymbal hook") || cmd.Name() == "version" || cmd.Name() == "help" {
		return true
	}
	if !cmd.Runnable() {
		return true
	}
	if getJSONFlag(cmd) {
		return true
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("CI")))
	return v == "1" || v == "true"
}

func isInteractiveTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
