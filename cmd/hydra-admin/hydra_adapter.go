package main

import (
	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

func newHydraAdapter(cfg *config.Config) (*hydra.ClusterAdapter, error) {
	configured := cfg.ConfiguredReplicas()
	replicas := make([]hydra.AdminReplica, 0, len(configured))
	for _, replica := range configured {
		replicas = append(replicas, hydra.AdminReplica{Name: replica.Name, URL: replica.AdminBaseURL})
	}
	adapter, err := hydra.NewClusterAdapter(replicas, cfg.Hydra.Timeout.Value())
	if err != nil {
		return nil, err
	}
	if cfg.Hydra.TokenRevokeEnabled {
		if err := adapter.ConfigurePublicRevoke(cfg.Hydra.PublicAPIBaseURL); err != nil {
			return nil, err
		}
	}
	return adapter, nil
}
