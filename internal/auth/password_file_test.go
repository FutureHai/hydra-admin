package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPasswordStateFileIsPrivateAndValidated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "password.hash")
	if _, err := LoadPasswordHash(path); err == nil {
		t.Fatal("missing password state file accepted")
	}
	next, err := GeneratePasswordHash([]byte("changed-password"), "bcrypt")
	if err != nil {
		t.Fatal(err)
	}
	if err := SavePasswordHash(path, next); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("password state file is not private: %v", err)
	}
	if got, err := LoadPasswordHash(path); err != nil || got != next {
		t.Fatalf("password state was not loaded: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPasswordHash(path); err == nil {
			t.Fatal("insecure password state accepted")
		}
	}
}
