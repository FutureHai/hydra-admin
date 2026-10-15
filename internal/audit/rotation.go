package audit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RotatingWriter struct {
	mu             sync.Mutex
	path           string
	file           *os.File
	size, maxBytes int64
	maxArchives    int
	mode           os.FileMode
	closed         bool
	rename         func(string, string) error
	remove         func(string) error
	now            func() time.Time
	sequence       uint64
}

func NewRotatingWriter(path string, maxBytes int64, maxArchives int, mode os.FileMode) (*RotatingWriter, error) {
	if path == "" || maxBytes < 1 || maxArchives < 1 || mode.Perm() != 0600 {
		return nil, errors.New("invalid rotating writer settings")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	w := &RotatingWriter{path: path, maxBytes: maxBytes, maxArchives: maxArchives, mode: mode, rename: os.Rename, remove: os.Remove, now: time.Now}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) open() error {
	if info, err := os.Lstat(w.path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("log file has unsafe mode or type")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, w.mode)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.file == nil {
		return 0, errors.New("rotating writer is closed or unavailable")
	}
	if int64(len(p)) > w.maxBytes {
		return 0, errors.New("record exceeds max_file_bytes")
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (w *RotatingWriter) rotateLocked() error {
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("sync before rotation: %w", err)
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return fmt.Errorf("close before rotation: %w", err)
	}
	w.file = nil
	w.sequence++
	archive := fmt.Sprintf("%s.%s.%06d", w.path, w.now().UTC().Format("20060102T150405.000000000Z"), w.sequence)
	if err := w.rename(w.path, archive); err != nil {
		_ = w.open()
		return fmt.Errorf("archive log: %w", err)
	}
	if err := w.open(); err != nil {
		return fmt.Errorf("open fresh log: %w", err)
	}
	if err := w.pruneLocked(); err != nil {
		return fmt.Errorf("prune archives: %w", err)
	}
	return nil
}

func (w *RotatingWriter) archivePathsLocked() ([]string, error) {
	entries, err := os.ReadDir(filepath.Dir(w.path))
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(w.path) + "."
	paths := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasPrefix(name, prefix) {
			suffix := strings.TrimPrefix(name, prefix)
			if len(suffix) >= len("20060102T150405.000000000Z.000001") && strings.Contains(suffix, "Z.") {
				paths = append(paths, filepath.Join(filepath.Dir(w.path), name))
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (w *RotatingWriter) pruneLocked() error {
	paths, err := w.archivePathsLocked()
	if err != nil {
		return err
	}
	for len(paths) > w.maxArchives {
		if err := w.remove(paths[0]); err != nil {
			return err
		}
		paths = paths[1:]
	}
	return nil
}

func (w *RotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return errors.New("rotating writer unavailable")
	}
	return w.file.Sync()
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Sync()
	closeErr := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

func (w *RotatingWriter) ArchivePaths() ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.archivePathsLocked()
}
