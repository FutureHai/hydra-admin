package hydra

import "context"

var businessNames = []string{
	"client_list", "client_detail", "client_create", "client_update", "client_lifespans_update", "client_delete",
	"secret_rotate", "token_introspect", "token_revoke", "pagination",
	"scopes", "audiences",
}

// Probe uses at most three fixed GET requests. The observed version is
// informational; business capabilities depend on the verified local contract
// and the upstream health and Client list probes.
func Probe(ctx context.Context, adapter HydraAdapter, registry Registry) (r Report) {
	r = Report{
		Status: "unavailable", DevelopmentBaseline: Version26, SpecSHA256: SpecSHA26,
		SpecValid: registry.SpecValid(), VersionProbe: ProbeUnavailable,
		HealthProbe: ProbeUnavailable, ClientListProbe: ProbeUnavailable,
		WriteAcceptance: "not_verified_in_this_deployment",
		Capabilities:    make(map[string]Capability),
	}
	// Local audit reading is independent of Hydra's version and health.
	r.Capabilities["audit_read"] = Capability{Enabled: true}
	for _, name := range businessNames {
		r.Capabilities[name] = Capability{Reason: "unavailable"}
	}
	// Collect the informational version last so a slow /version endpoint cannot
	// consume the probe deadline before readiness and Client list are checked.
	defer func() {
		version, result := adapter.Version(ctx)
		r.VersionProbe = result
		// Avoid reflecting arbitrary upstream content into diagnostics.
		if result == ProbeOK && len(version) <= 64 && safeVersion(version) {
			r.ObservedVersion = version
		}
	}()
	r.HealthProbe = adapter.Ready(ctx)
	if r.HealthProbe == ProbeDenied {
		r.Diagnostic = "上游拒绝访问；可能要求额外认证，本期不支持"
	}
	if !r.SpecValid || adapter.Tag() != Version26 || adapter.SpecSHA256() != SpecSHA26 {
		r.Diagnostic = "内置 Hydra 规范校验失败"
		setReasons(&r, "embedded_spec_invalid")
		return r
	}
	if r.HealthProbe != ProbeOK {
		if r.Diagnostic == "" {
			r.Diagnostic = "Hydra 健康检查未就绪"
		}
		setReasons(&r, "health_not_ready")
		return r
	}
	r.Status = "available"
	r.ClientListProbe = adapter.ClientListProbe(ctx)
	if r.ClientListProbe == ProbeDenied {
		r.Diagnostic = "上游拒绝访问；可能要求额外认证，本期不支持"
	}
	if r.ClientListProbe != ProbeOK && r.Diagnostic == "" {
		r.Diagnostic = "Client 列表只读探测未通过"
	}
	setReasons(&r, "not_implemented_in_this_phase")
	if r.ClientListProbe != ProbeOK {
		r.Capabilities["client_list"] = Capability{Reason: string(r.ClientListProbe)}
		r.Capabilities["client_detail"] = Capability{Reason: "client_list_probe_failed"}
	} else {
		r.Capabilities["client_list"] = Capability{Enabled: true}
		r.Capabilities["client_detail"] = Capability{Enabled: true}
		if _, ok := adapter.(ClientPageAdapter); ok {
			r.Capabilities["pagination"] = Capability{Enabled: true}
		}
		r.Capabilities["scopes"] = Capability{Enabled: true}
		r.Capabilities["audiences"] = Capability{Enabled: true}
		if _, ok := adapter.(ClientWriteAdapter); ok && registry.WriteValid() {
			for _, name := range []string{"client_create", "client_update", "client_delete"} {
				r.Capabilities[name] = Capability{Enabled: true}
			}
		}
		if _, ok := adapter.(ClientLifespanAdapter); ok && registry.LifespanWriteValid() {
			r.Capabilities["client_lifespans_update"] = Capability{Enabled: true}
		}
		r.Capabilities["secret_rotate"] = Capability{Reason: "rotation_operation_absent"}
		if tokenAdapter, ok := adapter.(TokenAdapter); ok && registry.TokenIntrospectValid() {
			r.Capabilities["token_introspect"] = Capability{Enabled: true}
			if registry.TokenRevokeValid() && tokenAdapter.PublicRevokeConfigured() {
				r.Capabilities["token_revoke"] = Capability{Enabled: true}
			} else {
				r.Capabilities["token_revoke"] = Capability{Reason: "public_revoke_not_configured"}
			}
		}
	}
	return r
}

func setReasons(r *Report, reason string) {
	for _, name := range businessNames {
		r.Capabilities[name] = Capability{Reason: reason}
	}
}

func safeVersion(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '.' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}
