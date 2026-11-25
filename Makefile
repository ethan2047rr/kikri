# Kikri build helpers. Requires Go and Node.js (for the TypeScript frontend).

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN     := bin
APP     := $(BIN)/Kikri.app

# Match LSMinimumSystemVersion in build/darwin/Info.plist.
ifeq ($(shell go env GOOS),darwin)
export MACOSX_DEPLOYMENT_TARGET := 13.0
export CGO_CFLAGS  := -mmacosx-version-min=13.0
export CGO_LDFLAGS := -mmacosx-version-min=13.0
endif

.PHONY: all frontend typecheck build run diagnose test vet app windows clean

all: build

frontend/node_modules: frontend/package.json
	cd frontend && npm install --no-audit --no-fund
	@touch $@

frontend: frontend/node_modules
	cd frontend && npm run -s build

typecheck: frontend/node_modules
	cd frontend && npm run -s typecheck

build: frontend
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kikri .

run: build
	./$(BIN)/kikri

diagnose: build
	./$(BIN)/kikri -diagnose

test:
	go test ./...

vet: typecheck
	go vet ./...

# macOS menu bar app bundle (unsigned, see docs: D12).
app: build
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS
	cp $(BIN)/kikri $(APP)/Contents/MacOS/kikri
	sed 's/__VERSION__/$(VERSION)/g' build/darwin/Info.plist > $(APP)/Contents/Info.plist

# Windows GUI binary (no console window), cross-compiled without cgo.
windows: frontend
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o $(BIN)/kikri.exe .

clean:
	rm -rf $(BIN) internal/gui/assets/app.js
