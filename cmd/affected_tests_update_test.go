package cmd

import (
	"os"
	"testing"
)

func TestAffectedTestsQuietUpdateNoticeSuppressed(t *testing.T) {
	old := interactiveTerminalFn
	interactiveTerminalFn = func(*os.File) bool { return true }
	t.Cleanup(func() { interactiveTerminalFn = old })
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "")
	t.Setenv("CI", "")
	command := newAffectedTestsCommand()
	if shouldSkipPassiveUpdateNotice(command) {
		t.Fatal("interactive normal text command must allow a passive notice")
	}
	if err := command.Flags().Set("quiet", "true"); err != nil {
		t.Fatal(err)
	}
	if !shouldSkipPassiveUpdateNotice(command) {
		t.Fatal("quiet output must suppress passive notices even with interactive stderr")
	}
}
