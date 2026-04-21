package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestShouldSkipPassiveUpdateNoticeForJSON(t *testing.T) {
	reset := stubPassiveNoticeEnv(t)
	defer reset()

	cmd := &cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}
	cmd.Flags().Bool("json", false, "")
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	rootCmd.AddCommand(cmd)
	defer rootCmd.RemoveCommand(cmd)

	if !shouldSkipPassiveUpdateNotice(cmd) {
		t.Fatal("expected --json command to skip passive notice")
	}
}

func TestShouldSkipPassiveUpdateNoticeForHookCommands(t *testing.T) {
	reset := stubPassiveNoticeEnv(t)
	defer reset()

	cmd := &cobra.Command{Use: "remind", Run: func(cmd *cobra.Command, args []string) {}}
	hookCmd.AddCommand(cmd)
	defer hookCmd.RemoveCommand(cmd)

	if !shouldSkipPassiveUpdateNotice(cmd) {
		t.Fatal("expected hook command to skip passive notice")
	}
}

func TestShouldSkipPassiveUpdateNoticeForCI(t *testing.T) {
	reset := stubPassiveNoticeEnv(t)
	defer reset()
	t.Setenv("CI", "true")

	cmd := &cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}
	cmd.Flags().Bool("json", false, "")
	rootCmd.AddCommand(cmd)
	defer rootCmd.RemoveCommand(cmd)

	if !shouldSkipPassiveUpdateNotice(cmd) {
		t.Fatal("expected CI to skip passive notice")
	}
}

func TestShouldSkipPassiveUpdateNoticeForNonInteractiveTerminal(t *testing.T) {
	reset := stubPassiveNoticeEnv(t)
	defer reset()
	interactiveTerminalFn = func(f *os.File) bool { return false }

	cmd := &cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}
	cmd.Flags().Bool("json", false, "")
	rootCmd.AddCommand(cmd)
	defer rootCmd.RemoveCommand(cmd)

	if !shouldSkipPassiveUpdateNotice(cmd) {
		t.Fatal("expected non-interactive stderr to skip passive notice")
	}
}

func stubPassiveNoticeEnv(t *testing.T) func() {
	t.Helper()
	oldInteractive := interactiveTerminalFn
	interactiveTerminalFn = func(f *os.File) bool { return true }
	return func() {
		interactiveTerminalFn = oldInteractive
		_ = os.Unsetenv("CI")
		_ = os.Unsetenv("CYMBAL_NO_UPDATE_NOTIFIER")
	}
}
