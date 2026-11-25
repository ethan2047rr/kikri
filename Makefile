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

.PHONY: all frontend typecheck build run diagnose test vet app windows clean icons dist dist-mac dist-windows

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
	mkdir -p $(APP)/Contents/Resources
	cp build/darwin/Kikri.icns $(APP)/Contents/Resources/Kikri.icns
	sed 's/__VERSION__/$(VERSION)/g' build/darwin/Info.plist > $(APP)/Contents/Info.plist
	# Ad-hoc signature: unsigned bundles cannot use system notifications.
	codesign --force --sign - $(APP)

# Re-render tray PNGs and the app icon from build/icons/*.svg (macOS 14+).
icons:
	swift build/icons/render.swift .
	iconutil -c icns build/darwin/Kikri.iconset -o build/darwin/Kikri.icns
	rm -rf build/darwin/Kikri.iconset

# Windows GUI binary (no console window), cross-compiled without cgo.
windows: frontend
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o $(BIN)/kikri.exe .

# Release archives in bin/dist (CI runs these on a macOS runner).
DIST := $(BIN)/dist

# Universal (Apple silicon + Intel) Kikri.app, zipped with ditto so the
# bundle's attributes and ad-hoc signature survive.
dist-mac: frontend
	rm -rf $(APP) $(DIST)/Kikri-*-macos.zip
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kikri-arm64 .
	CGO_ENABLED=1 GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/kikri-amd64 .
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	# Not bin/kikri: go build refuses to overwrite a universal binary.
	lipo -create -output $(APP)/Contents/MacOS/kikri $(BIN)/kikri-arm64 $(BIN)/kikri-amd64
	rm $(BIN)/kikri-arm64 $(BIN)/kikri-amd64
	cp build/darwin/Kikri.icns $(APP)/Contents/Resources/Kikri.icns
	sed 's/__VERSION__/$(VERSION)/g' build/darwin/Info.plist > $(APP)/Contents/Info.plist
	codesign --force --sign - $(APP)
	ditto -c -k --keepParent $(APP) $(DIST)/Kikri-$(VERSION)-macos.zip

dist-windows: windows
	rm -f $(DIST)/Kikri-*-windows-amd64.zip
	mkdir -p $(DIST)
	cd $(BIN) && zip -q -X dist/Kikri-$(VERSION)-windows-amd64.zip kikri.exe

dist: dist-mac dist-windows
	cd $(DIST) && shasum -a 256 Kikri-$(VERSION)-* > SHA256SUMS.txt

clean:
	rm -rf $(BIN) internal/gui/assets/app.js
