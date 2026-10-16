package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"hydra-admin/internal/auth"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return errors.New("duration must be a string with a unit")
	}
	v, err := time.ParseDuration(node.Value)
	if err != nil || v <= 0 {
		return errors.New("duration must be positive and include a valid unit")
	}
	*d = Duration(v)
	return nil
}

func (d Duration) Value() time.Duration { return time.Duration(d) }

type Config struct {
	Server struct {
		BindAddress       string   `yaml:"bind_address"`
		Port              int      `yaml:"port"`
		PublicBaseURL     string   `yaml:"public_base_url"`
		TrustedProxyCIDRs []string `yaml:"trusted_proxy_cidrs"`
		ReadTimeout       Duration `yaml:"read_timeout"`
		ReadHeaderTimeout Duration `yaml:"read_header_timeout"`
		WriteTimeout      Duration `yaml:"write_timeout"`
		IdleTimeout       Duration `yaml:"idle_timeout"`
	} `yaml:"server"`
	Hydra struct {
		AdminBaseURL       string         `yaml:"admin_base_url"`
		AdminReplicas      []HydraReplica `yaml:"admin_replicas"`
		PublicAPIBaseURL   string         `yaml:"public_api_base_url"`
		Timeout            Duration       `yaml:"timeout"`
		TokenRevokeEnabled bool           `yaml:"token_revoke_enabled"`
	} `yaml:"hydra"`
	ResourceCatalog ResourceCatalog `yaml:"resource_catalog"`
	Auth            struct {
		Username          string   `yaml:"username"`
		PasswordHash      string   `yaml:"password_hash"`
		SessionTTL        Duration `yaml:"session_ttl"`
		SessionCookieName string   `yaml:"session_cookie_name"`
		MaxSessions       int      `yaml:"max_sessions"`
		Login             struct {
			Window                 Duration `yaml:"window"`
			MaxAttemptsPerIP       int      `yaml:"max_attempts_per_ip"`
			MaxAttemptsPerUsername int      `yaml:"max_attempts_per_username"`
			LockoutDuration        Duration `yaml:"lockout_duration"`
		} `yaml:"login"`
	} `yaml:"auth"`
	Security struct {
		CSRFHeaderName    string `yaml:"csrf_header_name"`
		CookieSecure      bool   `yaml:"cookie_secure"`
		CookieSameSite    string `yaml:"cookie_same_site"`
		AllowInsecureHTTP bool   `yaml:"allow_insecure_http"`
		APIRateLimit      struct {
			ReadPerMinutePerIP  int `yaml:"read_per_minute_per_ip"`
			WritePerMinutePerIP int `yaml:"write_per_minute_per_ip"`
		} `yaml:"api_rate_limit"`
		RequestBodyMaxBytes int64 `yaml:"request_body_max_bytes"`
	} `yaml:"security"`
	Audit FileConfig `yaml:"audit"`
	Log   struct {
		FileConfig `yaml:",inline"`
		Level      string `yaml:"level"`
		Format     string `yaml:"format"`
	} `yaml:"log"`
}

// HydraReplica identifies one Admin API endpoint without exposing its URL in status reports.
type HydraReplica struct {
	Name         string `yaml:"name"`
	AdminBaseURL string `yaml:"admin_base_url"`
}

// ResourceCatalog describes the resource server scopes offered in Client forms.
// Hydra stores scope and audience as independent allow-lists; this catalog is
// presentation and validation metadata, not a token authorization policy.
type ResourceCatalog struct {
	GlobalScopes []string             `yaml:"global_scopes" json:"global_scopes"`
	Audiences    []ResourceDefinition `yaml:"audiences" json:"audiences"`
}

type ResourceDefinition struct {
	Audience string   `yaml:"audience" json:"audience"`
	Scopes   []string `yaml:"scopes" json:"scopes"`
}

func (r ResourceCatalog) Enabled() bool { return len(r.GlobalScopes) > 0 || len(r.Audiences) > 0 }

func (r ResourceCatalog) Validate() error {
	if len(r.Audiences) > 64 || len(r.GlobalScopes) > 64 {
		return errors.New("resource_catalog exceeds 64 entries")
	}
	seenScopes := make(map[string]bool)
	for _, scope := range r.GlobalScopes {
		if !validScopeToken(scope) || seenScopes[scope] {
			return errors.New("resource_catalog.global_scopes contains an invalid or duplicate scope")
		}
		seenScopes[scope] = true
	}
	seenAudiences := make(map[string]bool)
	for _, resource := range r.Audiences {
		if len(resource.Scopes) == 0 || len(resource.Scopes) > 64 || seenAudiences[resource.Audience] {
			return errors.New("resource_catalog.audiences contains an empty or duplicate resource")
		}
		_, err := validURL(resource.Audience)
		if err != nil || len(resource.Audience) > 2048 || strings.ContainsAny(resource.Audience, " \t\r\n") {
			return errors.New("resource_catalog.audiences contains an invalid audience URL")
		}
		seenAudiences[resource.Audience] = true
		for _, scope := range resource.Scopes {
			if !validScopeToken(scope) || seenScopes[scope] {
				return errors.New("resource_catalog has an invalid or multiply assigned scope")
			}
			seenScopes[scope] = true
		}
	}
	return nil
}

func validScopeToken(scope string) bool {
	if scope == "" || len(scope) > 2048 {
		return false
	}
	for _, b := range []byte(scope) {
		if b != '!' && (b < '#' || b > '[') && (b < ']' || b > '~') {
			return false
		}
	}
	return true
}

func (c *Config) ConfiguredReplicas() []HydraReplica {
	if len(c.Hydra.AdminReplicas) != 0 {
		return append([]HydraReplica(nil), c.Hydra.AdminReplicas...)
	}
	return []HydraReplica{{Name: "hydra-1", AdminBaseURL: c.Hydra.AdminBaseURL}}
}

type FileConfig struct {
	Path         string `yaml:"path"`
	MaxFileBytes int64  `yaml:"max_file_bytes"`
	MaxArchives  int    `yaml:"max_archives"`
	FileMode     string `yaml:"file_mode"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("config must contain one YAML document")
		}
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	configPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if cfg.Audit.Path == configPath || cfg.Log.Path == configPath {
		return nil, errors.New("log path must not overwrite configuration file")
	}
	return &cfg, nil
}

func (c *Config) Validate(configDir string) error {
	if err := c.ResourceCatalog.Validate(); err != nil {
		return err
	}
	bind := net.ParseIP(c.Server.BindAddress)
	if bind == nil {
		return errors.New("server.bind_address must be an IP address")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port must be 1..65535")
	}
	public, err := validURL(c.Server.PublicBaseURL)
	if err != nil {
		return fmt.Errorf("server.public_base_url: %w", err)
	}
	if public.Path != "" && public.Path != "/" {
		return errors.New("server.public_base_url must be an origin URL")
	}
	httpDirect, httpProxy := false, false
	if public.Scheme == "http" {
		if c.Security.CookieSecure {
			return errors.New("HTTP console URL requires cookie_secure=false")
		}
		switch {
		case bind.IsLoopback() && literalLoopback(public.Hostname()):
		case !c.Security.AllowInsecureHTTP:
			return errors.New("non-loopback HTTP console URL requires allow_insecure_http=true")
		case privateHTTPDirect(bind, public, c.Server.Port):
			httpDirect = true
		case !bind.IsLoopback() && !literalLoopback(public.Hostname()):
			httpProxy = true
		default:
			return errors.New("HTTP console bind and URL are incompatible")
		}
	} else if c.Security.AllowInsecureHTTP || !c.Security.CookieSecure {
		return errors.New("HTTPS console URL requires cookie_secure=true and allow_insecure_http=false")
	}
	hasRemoteProxy := false
	for _, cidr := range c.Server.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return errors.New("server.trusted_proxy_cidrs contains invalid CIDR")
		}
		if bind.IsLoopback() && !network.IP.IsLoopback() {
			return errors.New("remote trusted proxy cannot reach loopback bind_address")
		}
		if httpDirect && !network.IP.IsLoopback() {
			return errors.New("insecure HTTP direct must not trust remote proxy CIDRs")
		}
		if httpProxy && !network.IP.IsLoopback() {
			ones, bits := network.Mask.Size()
			if ones != bits {
				return errors.New("insecure HTTP proxy requires host-specific trusted_proxy_cidrs")
			}
			hasRemoteProxy = true
		}
	}
	if httpProxy && !hasRemoteProxy {
		return errors.New("insecure HTTP proxy requires a non-loopback trusted proxy IP")
	}
	for _, item := range []struct {
		name     string
		duration Duration
	}{
		{"server.read_timeout", c.Server.ReadTimeout}, {"server.read_header_timeout", c.Server.ReadHeaderTimeout},
		{"server.write_timeout", c.Server.WriteTimeout}, {"server.idle_timeout", c.Server.IdleTimeout},
		{"hydra.timeout", c.Hydra.Timeout}, {"auth.session_ttl", c.Auth.SessionTTL},
		{"auth.login.window", c.Auth.Login.Window}, {"auth.login.lockout_duration", c.Auth.Login.LockoutDuration},
	} {
		if item.duration <= 0 {
			return fmt.Errorf("%s must be positive", item.name)
		}
	}
	if (c.Hydra.AdminBaseURL == "") == (len(c.Hydra.AdminReplicas) == 0) {
		return errors.New("configure exactly one of hydra.admin_base_url or hydra.admin_replicas")
	}
	if len(c.Hydra.AdminReplicas) > 32 {
		return errors.New("hydra.admin_replicas exceeds 32 entries")
	}
	seenNames, seenURLs := map[string]bool{}, map[string]bool{}
	for _, replica := range c.ConfiguredReplicas() {
		if !validReplicaName(replica.Name) || seenNames[replica.Name] {
			return errors.New("hydra.admin_replicas has an invalid or duplicate name")
		}
		seenNames[replica.Name] = true
		endpoint, err := validURL(replica.AdminBaseURL)
		if err != nil {
			return fmt.Errorf("hydra Admin URL for %s: %w", replica.Name, err)
		}
		canonical := endpoint.String()
		if seenURLs[canonical] {
			return errors.New("hydra.admin_replicas contains a duplicate Admin URL")
		}
		seenURLs[canonical] = true
	}
	if c.Hydra.PublicAPIBaseURL != "" {
		if _, err := validHydraPublicURL(c.Hydra.PublicAPIBaseURL); err != nil {
			return fmt.Errorf("hydra.public_api_base_url: %w", err)
		}
	} else if c.Hydra.TokenRevokeEnabled {
		return errors.New("hydra.public_api_base_url is required when revoke is enabled")
	}
	if strings.TrimSpace(c.Auth.Username) == "" {
		return errors.New("auth.username is required")
	}
	if err := auth.ValidatePasswordHash(c.Auth.PasswordHash); err != nil {
		return fmt.Errorf("auth.password_hash: %w", err)
	}
	if c.Auth.SessionTTL <= 0 || c.Auth.MaxSessions < 1 {
		return errors.New("auth session_ttl and max_sessions must be positive")
	}
	if !validCookieName(c.Auth.SessionCookieName) || c.Auth.SessionCookieName == "hydra_admin_pre_auth" {
		return errors.New("auth.session_cookie_name is invalid or conflicts with pre-auth cookie")
	}
	if c.Auth.Login.MaxAttemptsPerIP < 1 || c.Auth.Login.MaxAttemptsPerUsername < 1 {
		return errors.New("login attempt limits must be positive")
	}
	if c.Security.CSRFHeaderName != "X-CSRF-Token" {
		return errors.New("security.csrf_header_name must be X-CSRF-Token")
	}
	if c.Security.CookieSameSite != "lax" {
		return errors.New("security.cookie_same_site must be lax")
	}
	if c.Security.APIRateLimit.ReadPerMinutePerIP < 1 || c.Security.APIRateLimit.WritePerMinutePerIP < 1 || c.Security.RequestBodyMaxBytes < 1 {
		return errors.New("security rate and body limits must be positive")
	}
	if err := c.Audit.validate("audit", configDir); err != nil {
		return err
	}
	if err := c.Log.FileConfig.validate("log", configDir); err != nil {
		return err
	}
	if c.Audit.Path == c.Log.Path {
		return errors.New("audit.path and log.path must differ")
	}
	if c.Log.Level != "debug" && c.Log.Level != "info" && c.Log.Level != "warn" && c.Log.Level != "error" {
		return errors.New("log.level is invalid")
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		return errors.New("log.format is invalid")
	}
	return nil
}

func validURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return nil, errors.New("must be a complete http(s) URL without credentials, query or fragment")
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("port is invalid")
		}
	}
	return u, nil
}

func privateHTTPDirect(bind net.IP, public *url.URL, port int) bool {
	if bind == nil || public == nil || public.Scheme != "http" || !bind.IsPrivate() {
		return false
	}
	publicIP := net.ParseIP(public.Hostname())
	publicPort := public.Port()
	if publicPort == "" {
		publicPort = "80"
	}
	return publicIP != nil && publicIP.Equal(bind) && publicPort == strconv.Itoa(port)
}

// HTTPProxyOnly reports whether non-loopback HTTP access must arrive from a trusted proxy.
// Call it only after validating the configuration.
func (c *Config) HTTPProxyOnly() bool {
	if !c.Security.AllowInsecureHTTP {
		return false
	}
	public, err := url.Parse(c.Server.PublicBaseURL)
	bind := net.ParseIP(c.Server.BindAddress)
	return err == nil && public != nil && public.Scheme == "http" && bind != nil && !bind.IsLoopback() &&
		!literalLoopback(public.Hostname()) && !privateHTTPDirect(bind, public, c.Server.Port)
}

func validHydraPublicURL(raw string) (*url.URL, error) {
	u, err := validURL(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" && !literalLoopback(u.Hostname()) {
		return nil, errors.New("HTTP Hydra Public URL requires a loopback IP literal; cross-host access requires HTTPS")
	}
	return u, nil
}

func literalLoopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }

func validReplicaName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r >= '0' && r <= '9' || r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

func validCookieName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (f *FileConfig) validate(prefix, configDir string) error {
	if f.Path == "" || f.MaxFileBytes < 256 || f.MaxArchives < 1 {
		return fmt.Errorf("%s path, max_file_bytes and max_archives are invalid", prefix)
	}
	mode, err := strconv.ParseUint(f.FileMode, 8, 32)
	if err != nil || mode != 0600 {
		return fmt.Errorf("%s.file_mode must be 0600", prefix)
	}
	base, err := filepath.Abs(configDir)
	if err != nil {
		return err
	}
	resolved := f.Path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
		rel, err := filepath.Rel(base, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s.path escapes config directory", prefix)
		}
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if filepath.Clean(resolved) == filepath.Join(base, "config.yaml") {
		return fmt.Errorf("%s.path must not overwrite config", prefix)
	}
	f.Path = resolved
	return nil
}

// CheckWritablePaths performs local, reversible filesystem checks only; it never opens a network connection.
func (c *Config) CheckWritablePaths() error {
	for _, item := range []struct {
		name string
		file FileConfig
	}{{"audit", c.Audit}, {"log", c.Log.FileConfig}} {
		dir := filepath.Dir(item.file.Path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("%s directory: %w", item.name, err)
		}
		probe, err := os.CreateTemp(dir, ".hydra-admin-check-*")
		if err != nil {
			return fmt.Errorf("%s directory is not writable: %w", item.name, err)
		}
		if err := probe.Close(); err != nil {
			return fmt.Errorf("%s probe close: %w", item.name, err)
		}
		if err := os.Remove(probe.Name()); err != nil {
			return fmt.Errorf("%s probe removal: %w", item.name, err)
		}
		if stat, err := os.Lstat(item.file.Path); err == nil {
			if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
				return fmt.Errorf("%s file has unsafe mode or type", item.name)
			}
			file, err := os.OpenFile(item.file.Path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				return fmt.Errorf("%s file is not writable: %w", item.name, err)
			}
			file.Close()
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s file: %w", item.name, err)
		}
	}
	return nil
}
