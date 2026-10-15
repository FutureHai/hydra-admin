package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxProbeBytes = 1 << 20
const maxClientReadBytes = 4 << 20

type readOperation uint8

type readResult struct {
	data  []byte
	links []string
}

const (
	readVersion readOperation = iota + 1
	readClientList
	readClientDetail
)

type HTTPAdapter struct {
	base   *url.URL
	client *http.Client
	public *HTTPAdapter
}

// ConfigurePublicRevoke is explicit. Without it the Public endpoint cannot be
// reached by business code, even if an Admin adapter exists.
func (a *HTTPAdapter) ConfigurePublicRevoke(baseURL string) error {
	public, err := NewHTTPAdapter(baseURL, a.client.Timeout)
	if err != nil {
		return err
	}
	a.public = public
	return nil
}

func NewHTTPAdapter(baseURL string, timeout time.Duration) (*HTTPAdapter, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" || timeout <= 0 {
		return nil, errors.New("invalid Hydra probe configuration")
	}
	if base.Scheme == "http" {
		ip := net.ParseIP(base.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("plaintext Hydra probe requires a loopback IP literal")
		}
	}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   min(timeout, 5*time.Second),
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: min(timeout, 5*time.Second)}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if base.Scheme == "http" {
			tcp, ok := conn.RemoteAddr().(*net.TCPAddr)
			if !ok || !tcp.IP.IsLoopback() {
				conn.Close()
				return nil, errors.New("Hydra connection left loopback")
			}
		}
		return conn, nil
	}
	return &HTTPAdapter{base: base, client: &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (*HTTPAdapter) Tag() string        { return Version26 }
func (*HTTPAdapter) SpecSHA256() string { return SpecSHA26 }

func (a *HTTPAdapter) Version(ctx context.Context) (string, ProbeResult) {
	data, result := a.get(ctx, "/version", "")
	if result != ProbeOK {
		return "", result
	}
	var value struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &value) != nil || !safeVersion(value.Version) || len(value.Version) > 64 {
		return "", ProbeUnexpected
	}
	return value.Version, ProbeOK
}

func (a *HTTPAdapter) Ready(ctx context.Context) ProbeResult {
	data, result := a.get(ctx, "/health/ready", "")
	if result != ProbeOK {
		return result
	}
	var value struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &value) != nil || value.Status != "ok" {
		return ProbeUnexpected
	}
	return ProbeOK
}

func (a *HTTPAdapter) ClientListProbe(ctx context.Context) ProbeResult {
	data, result := a.get(ctx, "/admin/clients", "page_size=1")
	if result != ProbeOK {
		return result
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '[' {
		return ProbeUnexpected
	}
	var clients []json.RawMessage
	if json.Unmarshal(data, &clients) != nil {
		return ProbeUnexpected
	}
	for _, raw := range clients {
		if _, err := decodeClient(raw); err != nil {
			return ProbeUnexpected
		}
	}
	return ProbeOK
}

// get is the sole outbound path and has a fixed operation whitelist.
func (a *HTTPAdapter) get(ctx context.Context, endpoint, query string) ([]byte, ProbeResult) {
	if !((endpoint == "/version" || endpoint == "/health/ready") && query == "") && !(endpoint == "/admin/clients" && query == "page_size=1") {
		return nil, ProbeUnexpected
	}
	u := *a.base
	u.Path = strings.TrimRight(a.base.Path, "/") + endpoint
	u.RawQuery = query
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ProbeUnavailable
	}
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return nil, ProbeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, ProbeDenied
	}
	if response.StatusCode != http.StatusOK {
		return nil, ProbeUnexpected
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, ProbeUnexpected
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProbeBytes+1))
	if err != nil || len(data) > maxProbeBytes {
		return nil, ProbeUnexpected
	}
	return data, ProbeOK
}

// readJSON is the business read whitelist. No caller supplies a method or
// upstream path; the selected operation fixes both before any network I/O.
func (a *HTTPAdapter) readJSON(ctx context.Context, operation readOperation, id string, pageSize int) (readResult, *ReadError) {
	if !NewRegistry().SpecValid() {
		return readResult{}, readFailure(409, "unsupported_version")
	}
	u := *a.base
	prefix := strings.TrimRight(a.base.Path, "/")
	switch operation {
	case readVersion:
		if id != "" || pageSize != 0 {
			return readResult{}, readFailure(400, "invalid_client_id")
		}
		u.Path = prefix + "/version"
	case readClientList:
		if pageSize < 1 || pageSize > 1000 || !validPageToken(id) {
			return readResult{}, readFailure(400, "invalid_page_token")
		}
		u.Path = prefix + "/admin/clients"
		query := url.Values{"page_size": {strconv.Itoa(pageSize)}}
		if id != "" {
			query.Set("page_token", id)
		}
		u.RawQuery = query.Encode()
	case readClientDetail:
		if !validClientID(id) || pageSize != 0 {
			return readResult{}, readFailure(400, "invalid_client_id")
		}
		u.Path = prefix + "/admin/clients/" + id
		u.RawPath = strings.TrimRight(a.base.EscapedPath(), "/") + "/admin/clients/" + url.PathEscape(id)
	default:
		return readResult{}, readFailure(502, "hydra_bad_response")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return readResult{}, readFailure(502, "hydra_bad_gateway")
	}
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
			return readResult{}, readFailure(504, "hydra_timeout")
		}
		return readResult{}, readFailure(502, "hydra_bad_gateway")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		if operation == readClientDetail {
			return readResult{}, readFailure(404, "client_not_found")
		}
		return readResult{}, readFailure(502, "hydra_bad_gateway")
	case http.StatusConflict:
		return readResult{}, readFailure(409, "hydra_conflict")
	case http.StatusUnauthorized, http.StatusForbidden:
		return readResult{}, readFailure(502, "hydra_upstream_denied")
	default:
		return readResult{}, readFailure(502, "hydra_bad_gateway")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return readResult{}, readFailure(502, "hydra_bad_response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxClientReadBytes+1))
	if err != nil || len(data) > maxClientReadBytes {
		return readResult{}, readFailure(502, "hydra_bad_response")
	}
	return readResult{data: data, links: response.Header.Values("Link")}, nil
}

// writeJSON is the only outbound mutation path. Method and path are fixed by
// the reviewed v26.2.0 contract; neither is supplied by a browser request.
// The HTTP client has no retry transport and redirects are never followed.
func (a *HTTPAdapter) writeJSON(ctx context.Context, method, id string, body []byte, success int) ([]byte, *ReadError) {
	if !NewRegistry().SpecValid() {
		return nil, readFailure(409, "unsupported_version")
	}
	if method == http.MethodPost {
		if id != "" || success != http.StatusCreated || len(body) == 0 {
			return nil, readFailure(400, "bad_request")
		}
	} else if method == http.MethodPatch {
		if !validClientID(id) || success != http.StatusOK || len(body) == 0 {
			return nil, readFailure(400, "bad_request")
		}
	} else if method == http.MethodDelete {
		if !validClientID(id) || success != http.StatusNoContent || len(body) != 0 {
			return nil, readFailure(400, "bad_request")
		}
	} else {
		return nil, readFailure(400, "bad_request")
	}
	u := *a.base
	u.Path = strings.TrimRight(a.base.Path, "/") + "/admin/clients"
	if id != "" {
		u.Path += "/" + id
		u.RawPath = strings.TrimRight(a.base.EscapedPath(), "/") + "/admin/clients/" + url.PathEscape(id)
	}
	request, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, readFailure(502, "hydra_bad_gateway")
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := a.client.Do(request)
	if err != nil {
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
			return nil, readFailure(504, "hydra_timeout")
		}
		return nil, readFailure(502, "hydra_bad_gateway")
	}
	defer response.Body.Close()
	if response.StatusCode != success {
		switch response.StatusCode {
		case http.StatusNotFound:
			return nil, readFailure(404, "client_not_found")
		case http.StatusConflict:
			return nil, readFailure(409, "hydra_conflict")
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return nil, readFailure(422, "hydra_validation_failed")
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, readFailure(502, "hydra_upstream_denied")
		default:
			return nil, readFailure(502, "hydra_bad_gateway")
		}
	}
	if success == http.StatusNoContent {
		return nil, nil
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, readFailure(502, "hydra_bad_response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxClientReadBytes+1))
	if err != nil || len(data) > maxClientReadBytes {
		return nil, readFailure(502, "hydra_bad_response")
	}
	return data, nil
}
