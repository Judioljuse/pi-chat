# internal/version/VERSION is the source of truth for the version.
VERSION := $(shell cat internal/version/VERSION)
LDFLAGS := -s -w
GOPROXY ?= https://goproxy.cn,direct
DATA ?= ./data

.PHONY: build web test run docker release clean version

# build embeds the committed web/dist; run `make web` after changing web-src/.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/pichat ./cmd/pichat

# NPM_REGISTRY overrides web-src/.npmrc and the hosts recorded in package-lock.json.
NPM_REGISTRY ?= https://registry.npmjs.org
web:
	cd web-src && npm ci --registry=$(NPM_REGISTRY) --replace-registry-host=always && npm run build

test:
	go vet ./...
	go test ./...

run: build
	./bin/pichat serve --listen 127.0.0.1:8080 --data $(DATA)

docker:
	docker build --build-arg GOPROXY=$(GOPROXY) $(if $(NPM_REGISTRY),--build-arg NPM_REGISTRY=$(NPM_REGISTRY)) -t pichat:$(VERSION) -t pichat:latest .

version:
	@echo $(VERSION)

# Same layout as the GitHub release (.github/workflows/release.yml).
release:
	@mkdir -p dist
	@for os in linux darwin; do for arch in amd64 arm64; do \
		echo "dist/pichat_$(VERSION)_$${os}_$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/pichat_$(VERSION)_$${os}_$$arch ./cmd/pichat || exit 1; \
	done; done
	cp listener/pichat-listen.mjs dist/
	cd dist && sha256sum pichat_* pichat-listen.mjs > SHA256SUMS

clean:
	rm -rf bin dist
