package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Test-only proxy topology: :18086 is a single edge; :18087 -> :18088 is
// an outer edge plus inner proxy. Both edges discard attacker-supplied XFF.
func main() {
	backend, _ := url.Parse("http://127.0.0.1:18085")
	inner, _ := url.Parse("http://127.0.0.1:18088")
	serve := func(port string, target *url.URL, forwarded func(*http.Request) string) {
		proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Header.Set("X-Forwarded-For", forwarded(pr.In))
			pr.Out.Header.Set("X-Forwarded-Proto", "http")
		}}
		go func() {
			if err := http.ListenAndServe("127.0.0.1:"+port, proxy); err != nil {
				panic(fmt.Errorf("proxy %s: %w", port, err))
			}
		}()
	}
	remote := func(r *http.Request) string { host, _, _ := net.SplitHostPort(r.RemoteAddr); return host }
	serve("18086", backend, remote)
	serve("18088", backend, func(r *http.Request) string {
		return strings.TrimSpace(r.Header.Get("X-Forwarded-For")) + ", " + remote(r)
	})
	outer := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(inner)
		pr.Out.Header.Set("X-Forwarded-For", remote(pr.In))
		pr.Out.Header.Set("X-Forwarded-Proto", "http")
	}}
	if err := http.ListenAndServe("127.0.0.1:18087", outer); err != nil {
		panic(err)
	}
}
