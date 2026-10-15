package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Test-only Hydra fixture. It is never included in a release.
func main() {
	var mu sync.Mutex
	clients := map[string]map[string]any{}
	jsonReply := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	h := http.NewServeMux()
	h.HandleFunc("GET /__fixture", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "hydra-admin-e2e-mock") })
	h.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, 200, map[string]string{"version": "v26.2.0"})
	})
	h.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) { jsonReply(w, 200, map[string]string{"status": "ok"}) })
	h.HandleFunc("GET /admin/clients", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		items := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			items = append(items, c)
		}
		jsonReply(w, 200, items)
	})
	h.HandleFunc("POST /admin/clients", func(w http.ResponseWriter, r *http.Request) {
		var client map[string]any
		if json.NewDecoder(r.Body).Decode(&client) != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		id, _ := client["client_id"].(string)
		if id == "" {
			http.Error(w, "missing id", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if _, exists := clients[id]; exists {
			http.Error(w, "duplicate", 409)
			return
		}
		client["fixture_unknown_field"] = "preserve-me"
		clients[id] = client
		jsonReply(w, 201, client)
	})
	h.HandleFunc("GET /admin/clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		client, ok := clients[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		jsonReply(w, 200, client)
	})
	h.HandleFunc("PATCH /admin/clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON Patch body required", 415)
			return
		}
		var operations []struct {
			Op    string `json:"op"`
			Path  string `json:"path"`
			Value any    `json:"value"`
		}
		if json.NewDecoder(r.Body).Decode(&operations) != nil {
			http.Error(w, "invalid patch", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		client, ok := clients[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		for _, op := range operations {
			if op.Op != "replace" && op.Op != "add" || !strings.HasPrefix(op.Path, "/") || op.Path == "/client_secret" {
				http.Error(w, "invalid patch operation", 400)
				return
			}
			client[strings.TrimPrefix(op.Path, "/")] = op.Value
		}
		jsonReply(w, 200, client)
	})
	h.HandleFunc("DELETE /admin/clients/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		delete(clients, r.PathValue("id"))
		w.WriteHeader(204)
	})
	// A PUT request indicates an unsafe regression in the adapter.
	h.HandleFunc("PUT /admin/clients/{id}", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "PUT forbidden", 405) })
	if err := http.ListenAndServe("127.0.0.1:14445", h); err != nil {
		panic(fmt.Errorf("mock Hydra: %w", err))
	}
}
