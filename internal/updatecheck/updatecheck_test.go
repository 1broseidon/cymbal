package updatecheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	if got := compareVersions("v0.11.5", "v0.12.0"); got >= 0 {
		t.Fatalf("expected newer latest version, got %d", got)
	}
	if got := compareVersions("0.12.0", "v0.12.0"); got != 0 {
		t.Fatalf("expected equal versions, got %d", got)
	}
	if got := compareVersions("dev", "v0.12.0"); got != 0 {
		t.Fatalf("expected uncheckable version to compare as equal, got %d", got)
	}
}

func TestDetectInstallTypeOverrideAndCommand(t *testing.T) {
	t.Setenv("CYMBAL_INSTALL_METHOD", "homebrew")
	t.Setenv("CYMBAL_UPDATE_COMMAND", "brew upgrade custom/cymbal")
	installType, cmd := detectInstall()
	if installType != InstallHomebrew {
		t.Fatalf("detectInstall() type = %q, want %q", installType, InstallHomebrew)
	}
	if cmd != "brew upgrade custom/cymbal" {
		t.Fatalf("detectInstall() command = %q", cmd)
	}
}

func TestDetectInstallTypePrefersDockerMarker(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()
	t.Setenv("CYMBAL_DOCKER_IMAGE", "1")
	execPathFn = func() (string, error) { return "/opt/homebrew/bin/cymbal", nil }
	evalSymlinks = func(path string) (string, error) { return "/opt/homebrew/Cellar/cymbal/0.11.5/bin/cymbal", nil }

	if got := detectInstallType(); got != InstallDocker {
		t.Fatalf("detectInstallType() = %q, want %q", got, InstallDocker)
	}
}

func TestDetectInstallTypeDoesNotAssumeHomebrewForGenericBinPath(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()
	execPathFn = func() (string, error) { return "/usr/local/bin/cymbal", nil }
	evalSymlinks = func(path string) (string, error) { return path, nil }

	if got := detectInstallType(); got != InstallManual {
		t.Fatalf("detectInstallType() = %q, want %q", got, InstallManual)
	}
}

func TestDetectInstallTypeRecognizesWindowsExeSuffixes(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()
	execPathFn = func() (string, error) { return `C:\Users\Lucas\go\bin\cymbal.exe`, nil }
	evalSymlinks = func(path string) (string, error) { return path, nil }

	if got := detectInstallType(); got != InstallGo {
		t.Fatalf("detectInstallType() = %q, want %q", got, InstallGo)
	}

	execPathFn = func() (string, error) { return `C:\Tools\cymbal.exe`, nil }
	if got := detectInstallType(); got != InstallManual {
		t.Fatalf("detectInstallType() = %q, want %q", got, InstallManual)
	}
}

func TestRenderCommandGoUsesWindowsSafeShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		cmd := renderCommand(InstallGo, "")
		if !strings.Contains(cmd, `powershell -NoProfile -Command`) {
			t.Fatalf("expected Windows-safe powershell wrapper, got %q", cmd)
		}
		return
	}
	cmd := renderCommand(InstallGo, "")
	if strings.Contains(cmd, `powershell -NoProfile -Command`) {
		t.Fatalf("unexpected Windows wrapper on non-Windows runtime: %q", cmd)
	}
}

func TestGetStatusUsesFreshCacheWithoutNetwork(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }

	state := cacheState{
		SchemaVersion:   schemaVersion,
		CurrentVersion:  "v0.11.5",
		LastCheckedAt:   now.Add(-time.Hour),
		LatestVersion:   "v0.12.0",
		ReleaseURL:      releaseURL,
		UpdateAvailable: true,
		InstallType:     InstallHomebrew,
		UpdateCommand:   renderCommand(InstallHomebrew, "v0.12.0"),
	}
	if err := saveState(state); err != nil {
		t.Fatal(err)
	}
	releaseFetch = func(ctx context.Context) (releaseInfo, error) {
		t.Fatal("release fetch should not be called for fresh cache")
		return releaseInfo{}, nil
	}

	status, err := GetStatus(context.Background(), Options{CurrentVersion: "v0.11.5", AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != "v0.12.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Source != "cache" {
		t.Fatalf("expected cache source, got %q", status.Source)
	}
	if status.Command != "brew upgrade 1broseidon/tap/cymbal" {
		t.Fatalf("unexpected update command %q", status.Command)
	}
}

func TestShouldNotifyHonorsThrottle(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	if err := saveState(cacheState{
		SchemaVersion:   schemaVersion,
		LastCheckedAt:   now,
		LatestVersion:   "v0.12.0",
		LastNotifiedAt:  now.Add(-time.Hour),
		LastNotifiedVer: "v0.12.0",
	}); err != nil {
		t.Fatal(err)
	}
	if ShouldNotify(Status{Available: true, LatestVersion: "v0.12.0"}) {
		t.Fatal("expected notification to be throttled")
	}
	if !ShouldNotify(Status{Available: true, LatestVersion: "v0.13.0"}) {
		t.Fatal("expected new version to bypass throttle")
	}
}

func stubUpdateCheckEnv(t *testing.T) func() {
	t.Helper()
	tempDir := t.TempDir()
	oldNow := nowFn
	oldCacheDir := cacheDirFn
	oldExecPath := execPathFn
	oldEvalSymlinks := evalSymlinks
	oldFetch := releaseFetch
	cacheDirFn = func() (string, error) { return tempDir, nil }
	execPathFn = func() (string, error) { return filepath.Join(tempDir, "bin", "cymbal"), nil }
	evalSymlinks = func(path string) (string, error) { return path, nil }
	releaseFetch = func(ctx context.Context) (releaseInfo, error) {
		return releaseInfo{Version: "v0.12.0", URL: releaseURL}, nil
	}
	return func() {
		nowFn = oldNow
		cacheDirFn = oldCacheDir
		execPathFn = oldExecPath
		evalSymlinks = oldEvalSymlinks
		releaseFetch = oldFetch
		_ = os.Unsetenv("CYMBAL_INSTALL_METHOD")
		_ = os.Unsetenv("CYMBAL_UPDATE_COMMAND")
		_ = os.Unsetenv("CYMBAL_NO_UPDATE_NOTIFIER")
		_ = os.Unsetenv("CYMBAL_DOCKER_IMAGE")
	}
	}
