BINARY = odf-io-stress
GOFLAGS = -trimpath

.PHONY: build test clean

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/odf-io-stress/

test:
	go test -race -count=1 ./...

clean:
	rm -f $(BINARY)
