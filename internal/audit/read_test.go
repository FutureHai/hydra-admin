package audit

import (
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
