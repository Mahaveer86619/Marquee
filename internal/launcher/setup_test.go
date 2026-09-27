package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetEnvValueCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := SetEnvValue(path, "MARQUEE_MEDIA_DIR", "D:/Media"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "MARQUEE_MEDIA_DIR=D:/Media\n")
}

func TestSetEnvValueReplacesAndKeepsOtherLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := "# comment\nOTHER=1\nMARQUEE_MEDIA_DIR=../media\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetEnvValue(path, "MARQUEE_MEDIA_DIR", "E:/Films"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "# comment\nOTHER=1\nMARQUEE_MEDIA_DIR=E:/Films\n")
}

func TestSetupRequiresComposeFile(t *testing.T) {
	err := Setup(t.Context(), SetupOptions{ComposeFile: filepath.Join(t.TempDir(), "missing.yaml"), Out: os.Stdout})
	if err == nil {
		t.Fatal("expected an error for a missing compose file")
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}
