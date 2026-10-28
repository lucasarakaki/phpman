BINARY  := phpman
VERSION ?= 0.2.0-dev
PREFIX  ?= /usr/local
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION)

E2E_IMAGE   ?= debian:12
E2E_BASE    ?= 8.2
E2E_TARGET  ?= 8.3
E2E_VERSION ?= 8.2

.PHONY: all build run test vet fmt fmt-check check tidy clean install e2e-build test-e2e test-rollback

all: build

build:
	@mkdir -p bin
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/phpman

run:
	go run ./cmd/phpman

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

fmt-check:
	@out="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt necessário em:"; echo "$$out"; exit 1; \
	fi

check: fmt-check vet test

e2e-build:
	@mkdir -p bin
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY)-e2e ./cmd/phpman

test-e2e: e2e-build
	PHPMAN_BIN=$(CURDIR)/bin/$(BINARY)-e2e bash test/e2e/run.sh $(E2E_IMAGE) $(E2E_BASE) $(E2E_TARGET)

test-rollback: e2e-build
	PHPMAN_BIN=$(CURDIR)/bin/$(BINARY)-e2e bash test/e2e/rollback.sh $(E2E_IMAGE) $(E2E_VERSION)

tidy:
	go mod tidy

clean:
	rm -rf bin

install: build
	install -m 0755 bin/$(BINARY) $(PREFIX)/bin/$(BINARY)
