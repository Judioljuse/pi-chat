VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w
GOPROXY ?= https://goproxy.cn,direct
DATA ?= ./data

.PHONY: build web test run docker release clean

# build embeds the committed web/dist; run `make web` after changing web-src/.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/pichat ./cmd/pichat

web:
	cd web-src && npm ci && npm run build

test:
	go vet ./...
	go test ./...

run: build
	./bin/pichat serve --listen 127.0.0.1:8080 --data $(DATA)

docker:
	docker build --build-arg GOPROXY=$(GOPROXY) $(if $(NPM_REGISTRY),--build-arg NPM_REGISTRY=$(NPM_REGISTRY)) -t pichat:$(VERSION) -t pichat:latest .

release:
	@mkdir -p dist
	@for os in linux darwin; do for arch in amd64 arm64; do \
		echo "dist/pichat-$$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/pichat-$$os-$$arch ./cmd/pichat || exit 1; \
	done; done
	cp listener/pichat-listen.mjs dist/
	cd dist && sha256sum pichat-* > SHA256SUMS

clean:
	rm -rf bin dist
