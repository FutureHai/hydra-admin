package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func LoadPasswordHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("password file is missing; create it with hash-password")
	}
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) || info.Size() == 0 || info.Size() > 512 {
		return "", errors.New("password file has unsafe mode, type or size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	hash := strings.TrimSuffix(string(data), "\n")
	if err := ValidatePasswordHash(hash); err != nil {
		return "", errors.New("password file contains an invalid hash")
	}
	return hash, nil
}

// SavePasswordHash replaces the file atomically, without exposing a partial hash.
func SavePasswordHash(path, hash string) error {
	if path == "" {
		return errors.New("password file path is required")
	}
	if err := ValidatePasswordHash(hash); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create password directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return errors.New("password directory has unsafe mode or type")
	}
	if old, err := os.Lstat(path); err == nil {
		if !old.Mode().IsRegular() || (runtime.GOOS != "windows" && old.Mode().Perm() != 0600) {
			return errors.New("password file has unsafe mode or type")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect password file: %w", err)
	}
	file, err := os.CreateTemp(dir, ".password-*")
	if err != nil {
		return fmt.Errorf("create temporary password file: %w", err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return fmt.Errorf("set password file permissions: %w", err)
	}
	if _, err := file.WriteString(hash + "\n"); err != nil {
		file.Close()
		return fmt.Errorf("write password file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync password file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close password file: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace password file: %w", err)
	}
	return nil
}
