package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// setEnvs is a test helper that sets multiple environment variables and returns
// a cleanup function that restores them.
func setEnvs(t *testing.T, envs map[string]string) {
	t.Helper()
	for k, v := range envs {
		t.Setenv(k, v)
	}
}

// clearConfigEnvs unsets all RDR_ environment variables relevant to Config.
// t.Setenv registers a cleanup that restores the original value after the test;
// os.Unsetenv then clears the variable for the duration of the current test.
func clearConfigEnvs(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"RDR_DATA_PATH",
		"RDR_DATABASE_PATH",
		"RDR_LISTEN_ADDR",
		"RDR_POLL_INTERVAL",
		"RDR_RETENTION_DAYS",
	} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearConfigEnvs(t)

	// Override RDR_DATA_PATH so we don't pollute the real home dir.
	tmp := t.TempDir()
	t.Setenv("RDR_DATA_PATH", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DataPath != tmp {
		t.Errorf("DataPath = %q, want %q", cfg.DataPath, tmp)
	}
	if want := filepath.Join(tmp, "rdr.db"); cfg.DatabasePath != want {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, want)
	}
	if want := filepath.Join(tmp, "favicons"); cfg.FaviconsDir != want {
		t.Errorf("FaviconsDir = %q, want %q", cfg.FaviconsDir, want)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.PollInterval != 6*time.Hour {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 6*time.Hour)
	}
	if cfg.RetentionDays != 0 {
		t.Errorf("RetentionDays = %d, want 0", cfg.RetentionDays)
	}
}

func TestLoad_AllOverrides(t *testing.T) {
	clearConfigEnvs(t)
	tmp := t.TempDir()
	setEnvs(t, map[string]string{
		"RDR_DATA_PATH":      tmp,
		"RDR_DATABASE_PATH":  "/tmp/test.db",
		"RDR_LISTEN_ADDR":    ":9090",
		"RDR_POLL_INTERVAL":  "30m",
		"RDR_RETENTION_DAYS": "90",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DataPath != tmp {
		t.Errorf("DataPath = %q, want %q", cfg.DataPath, tmp)
	}
	// RDR_DATABASE_PATH overrides the derived path.
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, "/tmp/test.db")
	}
	if want := filepath.Join(tmp, "favicons"); cfg.FaviconsDir != want {
		t.Errorf("FaviconsDir = %q, want %q", cfg.FaviconsDir, want)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.PollInterval != 30*time.Minute {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, 30*time.Minute)
	}
	if cfg.RetentionDays != 90 {
		t.Errorf("RetentionDays = %d, want 90", cfg.RetentionDays)
	}
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("RDR_DATA_PATH", t.TempDir())
	t.Setenv("RDR_POLL_INTERVAL", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid poll interval, got nil")
	}
}

func TestLoad_PollIntervalBelowMinimum(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("RDR_DATA_PATH", t.TempDir())
	t.Setenv("RDR_POLL_INTERVAL", "30s")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for poll interval below 1m, got nil")
	}
}

func TestLoad_InvalidRetentionDays(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("RDR_DATA_PATH", t.TempDir())
	t.Setenv("RDR_RETENTION_DAYS", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid retention days, got nil")
	}
}

// TestLoad_PollIntervalExactlyOneMinute verifies that a poll interval of exactly
// 1m is accepted. The boundary condition `d < time.Minute` must not be changed
// to `d <= time.Minute`, which would incorrectly reject the minimum valid value.
func TestLoad_PollIntervalExactlyOneMinute(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("RDR_DATA_PATH", t.TempDir())
	t.Setenv("RDR_POLL_INTERVAL", "1m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected 1m poll interval to be accepted, got error: %v", err)
	}
	if cfg.PollInterval != time.Minute {
		t.Errorf("PollInterval = %v, want %v", cfg.PollInterval, time.Minute)
	}
}

// TestLoad_DefaultDataPathAppendsRdr verifies that when RDR_DATA_PATH is not
// set, Load() derives DataPath by appending "rdr" to os.UserConfigDir(). This
// catches the mutation `filepath.Join(configDir, "rdr")` → `configDir`.
func TestLoad_DefaultDataPathAppendsRdr(t *testing.T) {
	clearConfigEnvs(t)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("os.UserConfigDir unavailable: %v", err)
	}

	wantDataPath := filepath.Join(configDir, "rdr")
	t.Cleanup(func() { _ = os.RemoveAll(wantDataPath) })

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DataPath != wantDataPath {
		t.Errorf("DataPath = %q, want %q", cfg.DataPath, wantDataPath)
	}
}

// TestLoad_FaviconsDirPermissions verifies that the favicons directory is
// created with mode 0o755. The umask is cleared so the OS does not mask any
// bits, allowing an exact permission check.
func TestLoad_FaviconsDirPermissions(t *testing.T) {
	clearConfigEnvs(t)
	tmp := t.TempDir()
	t.Setenv("RDR_DATA_PATH", tmp)

	// Clear umask so MkdirAll writes exactly the bits we specify.
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	_, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	faviconsDir := filepath.Join(tmp, "favicons")
	info, err := os.Stat(faviconsDir)
	if err != nil {
		t.Fatalf("stat favicons dir: %v", err)
	}

	const want = os.FileMode(0o755)
	got := info.Mode().Perm()
	if got != want {
		t.Errorf("favicons dir permissions = %04o, want %04o", got, want)
	}
}
