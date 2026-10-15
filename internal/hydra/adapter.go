package hydra

import "context"

// HydraAdapter exposes only allowlisted, read-only operations in this phase.
// Write operations require a separate reviewed interface and implementation.
type HydraAdapter interface {
	Tag() string
	SpecSHA256() string
	Version(context.Context) (string, ProbeResult)
	Ready(context.Context) ProbeResult
	ClientListProbe(context.Context) ProbeResult
	ListClients(context.Context) ([]ClientRead, *ReadError)
	GetClient(context.Context, string) (ClientRead, *ReadError)
}

// ClientPageAdapter is available only for reviewed versions with cursor paging.
type ClientPageAdapter interface {
	ListClientPage(context.Context, int, string) (ClientPage, *ReadError)
}

type ProbeResult string

const (
	ProbeOK          ProbeResult = "ok"
	ProbeUnavailable ProbeResult = "unavailable"
	ProbeDenied      ProbeResult = "upstream_access_denied"
	ProbeUnexpected  ProbeResult = "unexpected_response"
)

type Capability struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

type Report struct {
	Status           string                `json:"status"`
	Diagnostic       string                `json:"diagnostic,omitempty"`
	ObservedVersion  string                `json:"observed_version,omitempty"`
	SupportedVersion string                `json:"supported_version"`
	SpecSHA256       string                `json:"spec_sha256"`
	SpecValid        bool                  `json:"spec_valid"`
	VersionProbe     ProbeResult           `json:"version_probe"`
	HealthProbe      ProbeResult           `json:"health_probe"`
	ClientListProbe  ProbeResult           `json:"client_list_probe"`
	WriteAcceptance  string                `json:"write_acceptance"`
	Capabilities     map[string]Capability `json:"capabilities"`
	Replicas         []ReplicaStatus       `json:"replicas,omitempty"`
}

// ReplicaStatus contains only an operator-supplied label and safe probe results.
type ReplicaStatus struct {
	Name            string      `json:"name"`
	Status          string      `json:"status"`
	ObservedVersion string      `json:"observed_version,omitempty"`
	VersionProbe    ProbeResult `json:"version_probe"`
	HealthProbe     ProbeResult `json:"health_probe"`
	ClientListProbe ProbeResult `json:"client_list_probe"`
	Eligible        bool        `json:"eligible"`
}

func (r Report) BusinessAllowed(name string) bool {
	if r.Status != "supported" {
		return false
	}
	capability, ok := r.Capabilities[name]
	return ok && capability.Enabled
}
