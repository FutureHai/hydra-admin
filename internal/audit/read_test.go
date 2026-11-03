package audit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryFiltersPaginationAndRotatedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, err := NewRotatingWriter(path, 250, 4, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := NewRecorder(w)
	base := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	for i, action := range []string{"client.create", "client.update", "client.update", "client.delete"} {
		if err := r.Append(Event{Timestamp: base.Add(time.Duration(i) * time.Minute), Action: action, TargetType: "client", TargetID: "one", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	archives, err := w.ArchivePaths()
	if err != nil || len(archives) == 0 {
		t.Fatalf("rotation missing: %v %v", archives, err)
	}
	// A similarly named file is never part of the writer's archive set.
	if err := os.WriteFile(path+".other", []byte(`{"action":"client.update","outcome":"success","timestamp":"2026-10-13T01:00:00Z"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := r.Query(Query{Action: "client.update", ClientID: "one", From: base.Add(time.Minute), To: base.Add(2 * time.Minute), Limit: 1})
	if err != nil || len(page.Items) != 1 || !page.HasMore || !page.Items[0].Timestamp.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("first page: %+v %v", page, err)
	}
	if page.NextCursor == "" {
		t.Fatal("audit cursor missing")
	}
	query := Query{Action: "client.update", ClientID: "one", From: base.Add(time.Minute), To: base.Add(2 * time.Minute), Limit: 1, Cursor: page.NextCursor}
	page, err = r.Query(query)
	if err != nil || len(page.Items) != 1 || !page.Items[0].Timestamp.Equal(base.Add(time.Minute)) {
		t.Fatalf("cursor page: %+v %v", page, err)
	}
	query.Action = "client.delete"
	if _, err := r.Query(query); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor accepted with changed filter: %v", err)
	}
	page, err = r.Query(Query{Action: "client.update", ClientID: "one", Limit: 1, Offset: 1})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Timestamp.Equal(base.Add(time.Minute)) {
		t.Fatalf("second page: %+v %v", page, err)
	}
	if _, err := w.Write([]byte("not-json\n")); err != nil {
		t.Fatal(err)
	}
	page, err = r.Query(Query{Limit: 10})
	if err != nil || page.ParseErrors != 1 || len(page.Items) != 4 {
		t.Fatalf("malformed line isolation: %+v %v", page, err)
	}
	if _, err := r.Query(Query{Limit: 101}); err == nil {
		t.Fatal("unbounded limit accepted")
	}
}

func TestAuditCursorCanContinuePastScanWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, err := NewRotatingWriter(path, 10<<20, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	old := []byte(`{"timestamp":"2026-10-13T01:00:00Z","action":"old","outcome":"success"}` + "\n")
	noise := []byte(`{"timestamp":"2026-10-13T02:00:00Z","action":"noise","outcome":"success"}` + "\n")
	data := append(old, bytes.Repeat(noise, int(maxScanBytes/int64(len(noise)))+10)...)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(w)
	first, err := r.Query(Query{Action: "old", Limit: 1})
	if err != nil || len(first.Items) != 0 || first.NextCursor == "" || !first.ScanTruncated {
		t.Fatalf("first scan window: %+v %v", first, err)
	}
	second, err := r.Query(Query{Action: "old", Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].Action != "old" {
		t.Fatalf("continued scan: %+v %v", second, err)
	}
}

func TestAuditCursorExpiresWhenCurrentFileRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	w, err := NewRotatingWriter(path, 512, 2, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := NewRecorder(w)
	for i := 0; i < 2; i++ {
		if err := r.Append(Event{Action: "client.update", Outcome: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := r.Query(Query{Action: "client.update", Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("missing cursor: %+v %v", first, err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 500)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Query(Query{Action: "client.update", Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("rotated file cursor accepted: %v", err)
	}
}
