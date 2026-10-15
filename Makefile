.PHONY: web-install web-build build release test vet clean

web-install:
	cd web && npm ci

web-build:
	./scripts/build-web.sh

build: web-build
	mkdir -p dist
	go build -o dist/hydra-admin ./cmd/hydra-admin

release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=0.1.0" >&2; exit 2)
	./scripts/package-release.sh "$(VERSION)"

test:
	go test ./...
	cd web && npm run test
	cd web && npm run typecheck

vet:
	go vet ./...

clean:
	rm -rf dist web/dist
	find assets/dist -mindepth 1 -maxdepth 1 ! -name BUILD_WEB_FIRST.txt -exec rm -rf {} +
