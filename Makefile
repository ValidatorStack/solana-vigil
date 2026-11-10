BINARY := vigil
VERSION ?= v0.1.0
LDFLAGS := -s -w
DIST := dist

.PHONY: build test fmt vet cross clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/vigil

test:
	go test -race -count=1 ./...

fmt:
	gofmt -l .

vet:
	go vet ./...

cross:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/vigil-linux-amd64 ./cmd/vigil
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/vigil-linux-arm64 ./cmd/vigil
	cd $(DIST) && sha256sum vigil-linux-amd64 vigil-linux-arm64 > SHA256SUMS

clean:
	rm -rf $(DIST) $(BINARY)
