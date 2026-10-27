BINARY = odf-io-stress
GOFLAGS = -trimpath
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X main.version=$(VERSION)
DIST = dist
# Override IMAGE to push somewhere else, including GHCR:
#   make image-push IMAGE=ghcr.io/paraggit/odf-io-stress:dev
IMAGE ?= quay.io/paraggit/odf-io-stress:$(VERSION)

.PHONY: build test clean vet lint release-binaries image image-push

build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/odf-io-stress/

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint: vet
	@echo "Lint passed (go vet)"

# Cross-compile release artifacts locally (same matrix as CI).
release-binaries:
	@mkdir -p $(DIST)
	@for pair in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		goos=$${pair%/*}; goarch=$${pair#*/}; \
		out=$(DIST)/$(BINARY)_$(VERSION)_$${goos}_$${goarch}; \
		echo "Building $$out"; \
		GOOS=$${goos} GOARCH=$${goarch} CGO_ENABLED=0 \
			go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $$out ./cmd/odf-io-stress/; \
	done

image:
	docker build -f build/Dockerfile -t $(IMAGE) .

image-push: image
	docker push $(IMAGE)

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
