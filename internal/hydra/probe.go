package hydra

import "context"

var businessNames = []string{
	"client_list", "client_detail", "client_create", "client_update", "client_delete",
	"secret_rotate", "token_introspect", "token_revoke", "pagination",
	"scopes", "audiences",
}

// Probe performs only three fixed GET requests; Client list is skipped unless
// the observed version exactly matches the pinned, verified adapter.
func Probe(ctx context.Context, adapter HydraAdapter, registry Registry) Report {
	r := Report{
		Status: "unsupported_version", SupportedVersion: Version26, SpecSHA256: SpecSHA26,
		SpecValid: registry.SpecValid(), VersionProbe: ProbeUnavailable,
		HealthProbe: ProbeUnavailable, ClientListProbe: ProbeUnavailable,
		WriteAcceptance: "not_verified_in_this_deployment",
		Capabilities:    make(map[string]Capability),
	}
	// Local audit reading is independent of Hydra's version and health.
	r.Capabilities["audit_read"] = Capability{Enabled: true}
	for _, name := range businessNames {
		r.Capabilities[name] = Capability{Reason: "unsupported_version"}
	}
	version, versionResult := adapter.Version(ctx)
	r.VersionProbe = versionResult
	r.HealthProbe = adapter.Ready(ctx)
	if r.HealthProbe == ProbeDenied {
		r.Diagnostic = "上游拒绝访问；可能要求额外认证，本期不支持"
	}
	if versionResult != ProbeOK {
		r.Status = string(versionResult)
		if versionResult == ProbeDenied {
			r.Diagnostic = "上游拒绝访问；可能要求额外认证，本期不支持"
		}
		setReasons(&r, r.Status)
		return r
	}
	// Avoid reflecting arbitrary upstream content into diagnostics.
	if len(version) <= 64 && safeVersion(version) {
		r.ObservedVersion = version
	}
	if !registry.Match(version) || adapter.Tag() != version || adapter.SpecSHA256() != SpecSHA26 {
		r.Diagnostic = "运行版本与内置规范不匹配；Client/Token 操作已关闭"
		return r
	}
	r.Status = "supported"
	if r.HealthProbe != ProbeOK {
		if r.Diagnostic == "" {
			r.Diagnostic = "Hydra 健康检查未就绪"
		}
		setReasons(&r, "health_not_ready")
		return r
	}
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
