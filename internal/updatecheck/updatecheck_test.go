package updatecheck

import (
	"context"
	"errors"
	"io"
	"net/http"
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

func TestStatusFromStateIgnoresPersistedUpdateCommand(t *testing.T) {
	state := cacheState{
		SchemaVersion: schemaVersion,
		LatestVersion: "v0.12.0",
		ReleaseURL:    releaseURL,
		InstallType:   InstallHomebrew,
		UpdateCommand: "echo owned",
	}

	status := statusFromState(state, "v0.11.0", InstallUnknown, "")
	if status.Command != "brew upgrade 1broseidon/tap/cymbal" {
		t.Fatalf("expected structured homebrew command, got %q", status.Command)
	}
}

func TestMarkNotifiedAndFormatting(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	status := Status{
		Available:     true,
		LatestVersion: "v0.13.0",
		InstallType:   InstallGo,
		Command:       "go install ./cmd/cymbal",
		ReleaseURL:    releaseURL,
	}
	if err := MarkNotified(status); err != nil {
		t.Fatal(err)
	}
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.LastNotifiedAt.Equal(now) || state.LastNotifiedVer != "v0.13.0" {
		t.Fatalf("notification state not recorded: %+v", state)
	}
	if ShouldNotify(status) {
		t.Fatal("status should be throttled immediately after MarkNotified")
	}
	if notice := FormatNotice(status); !strings.Contains(notice, "v0.13.0") || !strings.Contains(notice, status.Command) {
		t.Fatalf("unexpected notice: %q", notice)
	}
	if FormatNotice(Status{}) != "" {
		t.Fatal("empty status should not format a notice")
	}

	augmented := AugmentReminder("remember this\n", status)
	if !strings.Contains(augmented, "cymbal update:") || !strings.Contains(augmented, status.Command) {
		t.Fatalf("unexpected augmented reminder: %q", augmented)
	}
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")
	if got := AugmentReminder("base", status); got != "base" {
		t.Fatalf("disabled notifier should leave reminder unchanged, got %q", got)
	}
}

func TestGetStatusFetchesLiveAndPreservesNotificationState(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	if err := saveState(cacheState{
		SchemaVersion:   schemaVersion,
		LastCheckedAt:   now.Add(-48 * time.Hour),
		LatestVersion:   "v0.12.0",
		LastNotifiedAt:  now.Add(-time.Hour),
		LastNotifiedVer: "v0.12.0",
	}); err != nil {
		t.Fatal(err)
	}
	releaseFetch = func(ctx context.Context) (releaseInfo, error) {
		return releaseInfo{Version: "v0.13.0", URL: "https://example.test/releases/v0.13.0"}, nil
	}

	status, err := GetStatus(context.Background(), Options{CurrentVersion: "v0.12.0", AllowNetwork: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if status.Source != "live" || !status.Available || status.LatestVersion != "v0.13.0" {
		t.Fatalf("unexpected live status: %+v", status)
	}
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastNotifiedVer != "v0.12.0" || state.LastNotifiedAt.IsZero() {
		t.Fatalf("notification state should be preserved across live refresh: %+v", state)
	}
}

func TestGetStatusUsesStaleCacheDuringFailureBackoff(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	if err := saveState(cacheState{
		SchemaVersion:     schemaVersion,
		LastCheckedAt:     now.Add(-48 * time.Hour),
		LastCheckFailedAt: now.Add(-time.Hour),
		LatestVersion:     "v0.12.0",
		ReleaseURL:        releaseURL,
	}); err != nil {
		t.Fatal(err)
	}
	releaseFetch = func(ctx context.Context) (releaseInfo, error) {
		t.Fatal("release fetch should be skipped during failure backoff")
		return releaseInfo{}, nil
	}

	status, err := GetStatus(context.Background(), Options{CurrentVersion: "v0.11.0", AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Source != "cache" || !status.CacheStale || !status.Available {
		t.Fatalf("unexpected stale-cache status: %+v", status)
	}
}

func TestGetStatusRecordsFailedRefreshForStaleCache(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	now := time.Date(2026, 4, 24, 13, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	if err := saveState(cacheState{
		SchemaVersion: schemaVersion,
		LastCheckedAt: now.Add(-48 * time.Hour),
		LatestVersion: "v0.12.0",
		ReleaseURL:    releaseURL,
	}); err != nil {
		t.Fatal(err)
	}
	releaseFetch = func(ctx context.Context) (releaseInfo, error) {
		return releaseInfo{}, errors.New("network down")
	}

	status, err := GetStatus(context.Background(), Options{CurrentVersion: "v0.11.0", AllowNetwork: true})
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if status.Source != "cache" || !status.CacheStale {
		t.Fatalf("unexpected fallback status: %+v", status)
	}
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.LastCheckFailedAt.Equal(now) {
		t.Fatalf("expected failed refresh timestamp, got %+v", state)
	}
}

func TestFetchLatestReleaseHTTPHandling(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != releaseLatest {
			t.Fatalf("unexpected URL: %s", req.URL)
		}
		if req.Header.Get("Accept") == "" || req.Header.Get("User-Agent") == "" {
			t.Fatalf("expected GitHub headers, got %v", req.Header)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v0.13.0","html_url":"https://example.test/v0.13.0"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	rel, err := fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.13.0" || rel.URL != "https://example.test/v0.13.0" {
		t.Fatalf("unexpected release info: %+v", rel)
	}

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := fetchLatestRelease(context.Background()); err == nil {
		t.Fatal("expected non-200 release response to fail")
	}
}

func TestInstallMarkerParseAndRenderCommands(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	marker, err := installMarkerPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte(`{"install_type":"docker"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadInstallMarker(); got != InstallDocker {
		t.Fatalf("loadInstallMarker() = %q, want docker", got)
	}
	if got := parseInstallType(" powershell "); got != InstallPowerShell {
		t.Fatalf("parseInstallType powershell = %q", got)
	}
	if got := parseInstallType("nonsense"); got != InstallUnknown {
		t.Fatalf("parseInstallType nonsense = %q", got)
	}
	if got := renderCommand(InstallDocker, "v0.13.0"); !strings.Contains(got, "v0.13.0") {
		t.Fatalf("docker command should include version, got %q", got)
	}
	if got := renderCommand(InstallPowerShell, ""); !strings.Contains(got, "install.ps1") {
		t.Fatalf("powershell command = %q", got)
	}
	if got := renderCommand(InstallManual, ""); got != releaseURL {
		t.Fatalf("manual command = %q", got)
	}
	t.Setenv("CYMBAL_UPDATE_COMMAND", "custom update")
	if got := renderCommand(InstallGo, ""); got != "custom update" {
		t.Fatalf("override command = %q", got)
	}
}

func TestGetStatusNoCacheNoNetworkAndNoopNotification(t *testing.T) {
	reset := stubUpdateCheckEnv(t)
	defer reset()

	status, err := GetStatus(context.Background(), Options{CurrentVersion: "v0.12.0", AllowNetwork: false})
	if err != nil {
		t.Fatal(err)
	}
	if status.Source != "none" || status.Available {
		t.Fatalf("unexpected no-cache status: %+v", status)
	}
	if err := MarkNotified(Status{Available: false, LatestVersion: "v0.99.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(); err == nil {
		t.Fatal("MarkNotified should not create state for unavailable status")
	}
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "yes")
	if !Disabled() {
		t.Fatal("CYMBAL_NO_UPDATE_NOTIFIER=yes should disable update checks")
	}
	if ShouldNotify(Status{Available: true, LatestVersion: "v0.99.0"}) {
		t.Fatal("disabled notifier should not notify")
	}
}

func TestAtomicWriteFileCleansUpAfterChmodFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod failure mode is platform-specific")
	}
	dir := filepath.Join(t.TempDir(), "missing")
	path := filepath.Join(dir, "state.json")
	if err := atomicWriteFile(path, []byte("{}"), 0o600); err == nil {
		t.Fatal("expected write into missing directory to fail")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file should be absent, stat err=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
