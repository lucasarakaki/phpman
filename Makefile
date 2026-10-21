BINARY  := phpman
VERSION ?= 0.1.0-dev
PREFIX  ?= /usr/local
GOFLAGS := -trimpath
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet fmt fmt-check check tidy clean install

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

tidy:
	go mod tidy

clean:
	rm -rf bin

install: build
	install -m 0755 bin/$(BINARY) $(PREFIX)/bin/$(BINARY)
