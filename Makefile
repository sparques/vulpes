.PHONY: all build check format test race vet

GOCACHE := /tmp/vulpes-go-cache
GOMODCACHE := /tmp/vulpes-go-mod-cache

all: check build

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -buildvcs=false ./...

check: test race vet

format:
	goimports -w $$(find cmd internal -name '*.go' -type f 2>/dev/null)

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

race:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...

vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...
