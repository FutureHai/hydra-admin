package auth

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func SafeMethod(method string) bool {
	return method == "GET" || method == "HEAD" || method == "OPTIONS"
}

func ValidateOrigin(origin, referer, publicBaseURL string) bool {
	base, err := url.Parse(publicBaseURL)
	if err != nil || base.Hostname() == "" {
		return false
	}
	if origin != "" {
		candidate, err := url.Parse(origin)
		return err == nil && candidate.User == nil && candidate.RawQuery == "" && candidate.Fragment == "" && candidate.Path == "" && sameOrigin(candidate, base)
	}
	if referer == "" {
		return false
	}
	candidate, err := url.Parse(referer)
	return err == nil && candidate.User == nil && sameOrigin(candidate, base)
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	if u.Scheme == "http" {
		return "80"
	}
	return ""
}

func ParseTrustedCIDRs(values []string) ([]*net.IPNet, error) {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, n, err := net.ParseCIDR(value)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, nil
}

func trusted(ip net.IP, ranges []*net.IPNet) bool {
	for _, cidr := range ranges {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// TrustedPeer checks the direct TCP peer, before any forwarded headers are read.
func TrustedPeer(remoteAddr string, ranges []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	return err == nil && trusted(net.ParseIP(host), ranges)
}

// ResolveClient walks X-Forwarded-For from right to left only while each hop is trusted.
// A trusted edge proxy must overwrite untrusted client-supplied forwarded headers.
func ResolveClient(remoteAddr string, tls bool, forwardedFor, forwardedProto string, trustedRanges []*net.IPNet) (net.IP, string, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil, "", errors.New("invalid remote address")
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return nil, "", errors.New("remote address is not an IP")
	}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	if !trusted(peer, trustedRanges) {
		return peer, scheme, nil
	}
	if forwardedProto != "" {
		if strings.Contains(forwardedProto, ",") {
			return nil, "", errors.New("forwarded proto must be overwritten by trusted edge proxy")
		}
		if forwardedProto != "http" && forwardedProto != "https" {
			return nil, "", errors.New("invalid forwarded proto")
		}
		scheme = forwardedProto
	}
	current := peer
	parts := strings.Split(forwardedFor, ",")
	for i := len(parts) - 1; i >= 0 && forwardedFor != ""; i-- {
		if !trusted(current, trustedRanges) {
			break
		}
		candidate := net.ParseIP(strings.TrimSpace(parts[i]))
		if candidate == nil {
			return nil, "", errors.New("invalid forwarded IP")
		}
		current = candidate
	}
	return current, scheme, nil
}
