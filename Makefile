BINARY = odf-io-stress
GOFLAGS = -trimpath
LDFLAGS = -s -w

.PHONY: build test clean vet lint

build:
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/odf-io-stress/

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint: vet
	@echo "Lint passed (go vet)"

clean:
	rm -f $(BINARY)
