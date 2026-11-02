PKG     := github.com/mnorrsken/scn3
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT)
WAILS   := go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.1

UNAME := $(shell uname -s)
ifeq ($(UNAME),Linux)
WAILS_TAGS := -tags webkit2_41
endif

.PHONY: build build-cli run test go-test ui-test vet fmt race lint vuln dist notices clean

# The desktop app, in build/bin.
build:
	$(WAILS) build -skipbindings $(WAILS_TAGS) -ldflags '$(LDFLAGS)'

# The CLI, in bin.
build-cli:
	go build -ldflags '$(LDFLAGS)' -o bin/scan ./cmd/scan

# The desktop app with live reload of the UI.
run:
	$(WAILS) dev -skipbindings $(WAILS_TAGS)

test: go-test ui-test

go-test:
	go test ./...

ui-test:
	cd frontend && npm ci && npm test

vet:
	go vet ./...

fmt:
	gofmt -w .

race:
	go test -race ./internal/... ./cmd/...

lint:
	golangci-lint run

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

# CLI binaries for every OS. The desktop app needs a native build per OS: see release.yml.
dist: vuln
	@mkdir -p dist
	@for t in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64 windows/arm64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "dist/scan-$$os-$$arch$$ext"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/scan-$$os-$$arch$$ext ./cmd/scan || exit 1; \
	done

# Licenses of the third-party code in the binaries; releases attach this file.
notices:
	@scripts/third-party-notices.sh > THIRD_PARTY_NOTICES.txt

clean:
	rm -rf bin dist build/bin frontend/wailsjs THIRD_PARTY_NOTICES.txt
