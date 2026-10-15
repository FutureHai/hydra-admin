package hydra

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type AdminReplica struct {
	Name string
	URL  string
}

type clusterReplica struct {
	name    string
	adapter *HTTPAdapter
}

// ClusterAdapter treats configured Admin endpoints as replicas of one logical
// Hydra deployment. One business operation is pinned to one endpoint; a failed
// write is never repeated against another replica.
type ClusterAdapter struct {
	replicas  []clusterReplica
	timeout   time.Duration
	next      atomic.Uint64
	mu        sync.RWMutex
	refreshMu sync.Mutex
	last      Report
	checked   time.Time
}

func NewClusterAdapter(endpoints []AdminReplica, timeout time.Duration) (*ClusterAdapter, error) {
	if len(endpoints) == 0 || len(endpoints) > 32 || timeout <= 0 {
		return nil, errors.New("invalid Hydra replica configuration")
	}
	cluster := &ClusterAdapter{timeout: timeout}
	for _, endpoint := range endpoints {
		if endpoint.Name == "" {
			return nil, errors.New("Hydra replica name is required")
		}
		adapter, err := NewHTTPAdapter(endpoint.URL, timeout)
		if err != nil {
			return nil, err
		}
		cluster.replicas = append(cluster.replicas, clusterReplica{name: endpoint.Name, adapter: adapter})
	}
	return cluster, nil
}

func (a *ClusterAdapter) ConfigurePublicRevoke(baseURL string) error {
	for _, replica := range a.replicas {
		if err := replica.adapter.ConfigurePublicRevoke(baseURL); err != nil {
			return err
		}
	}
	return nil
}

func (*ClusterAdapter) Tag() string        { return Version26 }
func (*ClusterAdapter) SpecSHA256() string { return SpecSHA26 }

func (a *ClusterAdapter) Report(ctx context.Context) Report {
	a.mu.RLock()
	if !a.checked.IsZero() {
		report := a.last
		stale := time.Since(a.checked) >= 3*time.Second
		a.mu.RUnlock()
		// A slow or unreachable replica must not delay healthy business traffic.
		// The previous snapshot can only nominate candidates; choose also checks
		// readiness and version against the chosen endpoint immediately.
		if stale && a.refreshMu.TryLock() {
			go func() {
				defer a.refreshMu.Unlock()
				probeCtx, cancel := context.WithTimeout(context.Background(), a.timeout)
				defer cancel()
				a.probeAndStore(probeCtx)
			}()
		}
		return report
	}
	a.mu.RUnlock()
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	a.mu.RLock()
	if !a.checked.IsZero() {
		report := a.last
		a.mu.RUnlock()
		return report
	}
	a.mu.RUnlock()
	return a.probeAndStore(ctx)
}

func (a *ClusterAdapter) probeAndStore(ctx context.Context) Report {
	reports := make([]Report, len(a.replicas))
	var wg sync.WaitGroup
	for i, replica := range a.replicas {
		wg.Add(1)
		go func(i int, adapter *HTTPAdapter) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, a.timeout)
			defer cancel()
			reports[i] = Probe(probeCtx, adapter, NewRegistry())
		}(i, replica.adapter)
	}
	wg.Wait()
	selected := 0
	statuses := make([]ReplicaStatus, len(reports))
	for i, report := range reports {
		eligible := report.BusinessAllowed("client_list")
		if eligible && !reports[selected].BusinessAllowed("client_list") {
			selected = i
		}
		statuses[i] = ReplicaStatus{
			Name: a.replicas[i].name, Status: report.Status, ObservedVersion: report.ObservedVersion,
			VersionProbe: report.VersionProbe, HealthProbe: report.HealthProbe,
			ClientListProbe: report.ClientListProbe, Eligible: eligible,
		}
	}
	result := reports[selected]
	result.Replicas = statuses
	if !result.BusinessAllowed("client_list") {
		result.Diagnostic = "没有健康且版本匹配的 Hydra 副本"
	}
	a.mu.Lock()
	a.last, a.checked = result, time.Now()
	a.mu.Unlock()
	return result
}

func (a *ClusterAdapter) choose(ctx context.Context, capability string) (*HTTPAdapter, *ReadError) {
	report := a.Report(ctx)
	if !report.BusinessAllowed(capability) {
		if report.Status == "unsupported_version" {
			return nil, readFailure(409, "unsupported_version")
		}
		return nil, readFailure(503, "unavailable")
	}
	start := int(a.next.Add(1)-1) % len(a.replicas)
	for offset := range a.replicas {
		i := (start + offset) % len(a.replicas)
		if !report.Replicas[i].Eligible {
			continue
		}
		adapter := a.replicas[i].adapter
		if adapter.Ready(ctx) != ProbeOK {
			continue
		}
		version, probe := adapter.Version(ctx)
		if probe == ProbeOK && NewRegistry().Match(version) {
			return adapter, nil
		}
	}
	return nil, readFailure(503, "unavailable")
}

func (a *ClusterAdapter) Version(ctx context.Context) (string, ProbeResult) {
	report := a.Report(ctx)
	return report.ObservedVersion, report.VersionProbe
}

func (a *ClusterAdapter) Ready(ctx context.Context) ProbeResult { return a.Report(ctx).HealthProbe }

func (a *ClusterAdapter) ClientListProbe(ctx context.Context) ProbeResult {
	return a.Report(ctx).ClientListProbe
}

func (a *ClusterAdapter) ListClients(ctx context.Context) ([]ClientRead, *ReadError) {
	adapter, err := a.choose(ctx, "client_list")
	if err != nil {
		return nil, err
	}
	return adapter.ListClients(ctx)
}

func (a *ClusterAdapter) ListClientPage(ctx context.Context, size int, token string) (ClientPage, *ReadError) {
	adapter, err := a.choose(ctx, "pagination")
	if err != nil {
		return ClientPage{}, err
	}
	return adapter.ListClientPage(ctx, size, token)
}

func (a *ClusterAdapter) GetClient(ctx context.Context, id string) (ClientRead, *ReadError) {
	adapter, err := a.choose(ctx, "client_detail")
	if err != nil {
		return ClientRead{}, err
	}
	return adapter.GetClient(ctx, id)
}

func (a *ClusterAdapter) CreateClient(ctx context.Context, fields map[string]json.RawMessage) (CreateResult, *ReadError) {
	adapter, err := a.choose(ctx, "client_create")
	if err != nil {
		return CreateResult{}, err
	}
	return adapter.CreateClient(ctx, fields)
}

func (a *ClusterAdapter) PatchClient(ctx context.Context, id, baseline string, changes map[string]json.RawMessage) (ClientRead, *ReadError) {
	adapter, err := a.choose(ctx, "client_update")
	if err != nil {
		return ClientRead{}, err
	}
	return adapter.PatchClient(ctx, id, baseline, changes)
}

func (a *ClusterAdapter) DeleteClient(ctx context.Context, id string) *ReadError {
	adapter, err := a.choose(ctx, "client_delete")
	if err != nil {
		return err
	}
	return adapter.DeleteClient(ctx, id)
}

func (a *ClusterAdapter) PublicRevokeConfigured() bool {
	return len(a.replicas) != 0 && a.replicas[0].adapter.PublicRevokeConfigured()
}

func (a *ClusterAdapter) IntrospectToken(ctx context.Context, token string) (TokenInfo, *ReadError) {
	adapter, err := a.choose(ctx, "token_introspect")
	if err != nil {
		return TokenInfo{}, err
	}
	return adapter.IntrospectToken(ctx, token)
}

func (a *ClusterAdapter) RevokeToken(ctx context.Context, token, clientID, secret string) *ReadError {
	adapter, err := a.choose(ctx, "token_revoke")
	if err != nil {
		return err
	}
	return adapter.RevokeToken(ctx, token, clientID, secret)
}
