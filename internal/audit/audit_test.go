package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hydra-admin/internal/observability"
)

func TestAuditJSONLConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, err := NewRotatingWriter(path, 32768, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := NewRecorder(w)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Append(Event{Action: "login", Outcome: "success", TargetID: "/x?token=TEST_ONLY_SECRET"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "TEST_ONLY_SECRET") {
		t.Fatal("audit query leaked")
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	count := 0
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Action != "login" || event.Outcome != "success" || event.Timestamp.IsZero() {
			t.Fatal("malformed audit event")
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 32 {
		t.Fatalf("got %d audit records", count)
	}
	if stat, _ := os.Stat(path); stat.Mode().Perm() != 0600 {
		t.Fatal("unsafe audit mode")
	}
}

func TestRotationCapsArchivesAndReportsFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, err := NewRotatingWriter(path, 100, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := w.Write([]byte(strings.Repeat("x", 60) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	archives, err := w.ArchivePaths()
	if err != nil || len(archives) != 2 {
		t.Fatalf("archives: %d %v", len(archives), err)
	}
	for _, p := range append(archives, path) {
		if stat, err := os.Stat(p); err != nil || stat.Size() > 100 {
			t.Fatalf("oversized or missing log %s: %v", p, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("closed")); err == nil {
		t.Fatal("closed writer accepted write")
	}

	failPath := filepath.Join(t.TempDir(), "app.log")
	failWriter, err := NewRotatingWriter(failPath, 64, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer failWriter.Close()
	failWriter.rename = func(string, string) error { return errors.New("injected rotation failure") }
	if _, err := failWriter.Write([]byte(strings.Repeat("a", 40))); err != nil {
		t.Fatal(err)
	}
	var alarm bytes.Buffer
	app := NewAppLogWriter(failWriter, &alarm)
	if _, err := app.Write([]byte(strings.Repeat("b", 40))); err == nil {
		t.Fatal("rotation failure swallowed")
	}
	if app.Healthy() || !strings.Contains(alarm.String(), "failed") {
		t.Fatal("app log failure not reported")
	}
}

func TestAppLogRotationAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := NewRotatingWriter(path, 256, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	app := NewAppLogWriter(w, nil)
	logger := slog.New(observability.NewRedactingHandler(slog.NewJSONHandler(app, nil)))
	for i := 0; i < 8; i++ {
		logger.Info("request", "access_token", "TEST_ONLY_SECRET", "time", time.Unix(int64(i), 0))
	}
	if !app.Healthy() {
		t.Fatal("app log unhealthy")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	archives, err := w.ArchivePaths()
	if err != nil || len(archives) != 2 {
		t.Fatalf("app archives: %d %v", len(archives), err)
	}
	for _, p := range append(archives, path) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "TEST_ONLY_SECRET") || int64(len(data)) > 256 {
			t.Fatalf("unsafe app log: %s", p)
		}
	}
}
