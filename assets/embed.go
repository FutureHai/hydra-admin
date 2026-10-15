package assets

import "embed"

// Dist is populated by scripts/build-web.sh before a real build.
// The tracked placeholder keeps go:embed valid in a fresh checkout.
//go:embed dist/*
var Dist embed.FS
