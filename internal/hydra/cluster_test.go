package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func clusterFixture(t *testing.T, version string, ready bool, writes *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case "/health/ready":
			if !ready {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			if r.Method != http.MethodGet || r.URL.Query().Get("page_size") != "1" {
				t.Errorf("unexpected list probe: %s %s", r.Method, r.URL.String())
			}
			w.Write([]byte(`[]`))
		case "/admin/clients/test-client":
			if r.Method != http.MethodDelete {
				t.Errorf("unexpected business method %s", r.Method)
			}
			writes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestClusterRoutesOnlyToHealthyCompatibleReplica(t *testing.T) {
	var badWrites, goodWrites atomic.Int32
	bad := clusterFixture(t, "v26.2.1", true, &badWrites)
	defer bad.Close()
	good := clusterFixture(t, Version26, true, &goodWrites)
	defer good.Close()
	cluster, err := NewClusterAdapter([]AdminReplica{{"bad", bad.URL}, {"good", good.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	report := cluster.Report(context.Background())
	if !report.BusinessAllowed("client_delete") || len(report.Replicas) != 2 || report.Replicas[0].Eligible || !report.Replicas[1].Eligible || report.Replicas[0].ObservedVersion != "v26.2.1" {
		t.Fatalf("wrong cluster report: %+v", report)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), bad.URL) || strings.Contains(string(encoded), good.URL) {
		t.Fatal("cluster report leaked Admin URLs")
	}
	if err := cluster.DeleteClient(context.Background(), "test-client"); err != nil || badWrites.Load() != 0 || goodWrites.Load() != 1 {
		t.Fatalf("write not isolated to compatible replica: %v %d %d", err, badWrites.Load(), goodWrites.Load())
	}
}

func TestClusterDistributesSeparateOperationsAcrossHealthyReplicas(t *testing.T) {
	var firstWrites, secondWrites atomic.Int32
	first := clusterFixture(t, Version26, true, &firstWrites)
	defer first.Close()
	second := clusterFixture(t, Version26, true, &secondWrites)
	defer second.Close()
	cluster, err := NewClusterAdapter([]AdminReplica{{"first", first.URL}, {"second", second.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := cluster.DeleteClient(context.Background(), "test-client"); err != nil {
			t.Fatal(err)
		}
	}
	if firstWrites.Load() != 1 || secondWrites.Load() != 1 {
		t.Fatalf("operations were not distributed: first=%d second=%d", firstWrites.Load(), secondWrites.Load())
	}
}

func TestClusterSkipsUnhealthyReplicaAndDoesNotRetryWrite(t *testing.T) {
	var downWrites, goodWrites atomic.Int32
	down := clusterFixture(t, Version26, false, &downWrites)
	defer down.Close()
	good := clusterFixture(t, Version26, true, &goodWrites)
	defer good.Close()
	cluster, err := NewClusterAdapter([]AdminReplica{{"down", down.URL}, {"good", good.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := cluster.DeleteClient(context.Background(), "test-client"); err != nil || downWrites.Load() != 0 || goodWrites.Load() != 1 {
		t.Fatalf("unhealthy replica received write: %v %d %d", err, downWrites.Load(), goodWrites.Load())
	}
	var firstWrites, secondWrites atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`[]`))
		case "/admin/clients/test-client":
			firstWrites.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer first.Close()
	second := clusterFixture(t, Version26, true, &secondWrites)
	defer second.Close()
	cluster, err = NewClusterAdapter([]AdminReplica{{"first", first.URL}, {"second", second.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := cluster.DeleteClient(context.Background(), "test-client"); err == nil || firstWrites.Load() != 1 || secondWrites.Load() != 0 {
		t.Fatalf("failed write was retried: %v %d %d", err, firstWrites.Load(), secondWrites.Load())
	}
}

func TestClusterRecoversAfterAllReplicasWereUnavailable(t *testing.T) {
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/health/ready":
			if !ready.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	cluster, err := NewClusterAdapter([]AdminReplica{{"recovering", server.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if cluster.Report(context.Background()).BusinessAllowed("client_list") {
		t.Fatal("unhealthy replica enabled Client operations")
	}
	ready.Store(true)
	cluster.mu.Lock()
	cluster.checked = time.Time{}
	cluster.mu.Unlock()
	if !cluster.Report(context.Background()).BusinessAllowed("client_list") {
		t.Fatal("recovered replica stayed disabled")
	}
}

func TestStaleStatusCannotSendWriteToFailedReplica(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/health/ready":
			if !ready.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`[]`))
		case "/admin/clients/test-client":
			writes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	cluster, err := NewClusterAdapter([]AdminReplica{{"node", server.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !cluster.Report(context.Background()).BusinessAllowed("client_delete") {
		t.Fatal("healthy fixture was not selected")
	}
	ready.Store(false)
	cluster.mu.Lock()
	cluster.checked = time.Now().Add(-4 * time.Second)
	cluster.mu.Unlock()
	if err := cluster.DeleteClient(context.Background(), "test-client"); err == nil || writes.Load() != 0 {
		t.Fatalf("stale report permitted a write: %v, calls=%d", err, writes.Load())
	}
	deadline := time.Now().Add(2 * time.Second)
	for cluster.Report(context.Background()).BusinessAllowed("client_list") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if cluster.Report(context.Background()).BusinessAllowed("client_list") {
		t.Fatal("background probe did not refresh failed replica")
	}
}
