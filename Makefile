VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
TAGS     = production
TARGETS  = darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

# The GUI links the platform webview through cgo, so it is built natively.
# `nogui` builds the terminal-only magpie, which cross-compiles anywhere.
ifeq ($(shell uname -s),Darwin)
  export CGO_CFLAGS  = -mmacosx-version-min=11.0
  export CGO_LDFLAGS = -mmacosx-version-min=11.0
endif

.PHONY: build cli install test app icons release release-cli clean dev dev-once

build:
	go build -tags $(TAGS) -trimpath -ldflags="$(LDFLAGS)" -o magpie .

cli:
	CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o magpie .

install:
	go install -tags $(TAGS) -trimpath -ldflags="$(LDFLAGS)" .

test:
	go vet ./... && go test ./...

# macOS bundle: menu bar app with no Dock icon (LSUIElement).
app: build
	@rm -rf magpie.app
	@mkdir -p magpie.app/Contents/MacOS magpie.app/Contents/Resources
	@cp magpie magpie.app/Contents/MacOS/magpie
	@cp build/darwin/magpie.icns magpie.app/Contents/Resources/magpie.icns
	@sed 's/@VERSION@/$(VERSION)/' build/darwin/Info.plist > magpie.app/Contents/Info.plist
	@echo "  magpie.app"

icons:
	@go run build/icon/gen.go tray internal/gui/tray.png
	@go run build/icon/gen.go app 64 internal/gui/icon.png
	@rm -rf build/darwin/magpie.iconset && mkdir -p build/darwin/magpie.iconset
	@for s in 16 32 128 256 512; do \
		go run build/icon/gen.go app $$s build/darwin/magpie.iconset/icon_$${s}x$${s}.png; \
		go run build/icon/gen.go app $$((s*2)) build/darwin/magpie.iconset/icon_$${s}x$${s}@2x.png; \
	done
	@iconutil -c icns build/darwin/magpie.iconset -o build/darwin/magpie.icns && rm -rf build/darwin/magpie.iconset

release: clean build
	@mkdir -p dist
	@cp magpie dist/magpie-$(shell go env GOOS)-$(shell go env GOARCH)
	@$(MAKE) --no-print-directory release-cli

release-cli:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ $$os = windows ] && ext=.exe; \
		echo "  $$os/$$arch (cli)"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o dist/magpie-cli-$$os-$$arch$$ext . ; \
	done

clean:
	rm -rf magpie magpie.exe magpie.app dist

# Development: the UI is served from internal/gui/assets and the window
# reloads itself when a file there is saved; with fswatch installed, Go
# changes rebuild and relaunch the app. Uses its own gateway port so a
# running magpie keeps serving the agents. Ctrl-C ends the loop: fswatch
# swallows the interrupt, so without the trap the shell would just go round
# again.
DEV_ADDR ?= 127.0.0.1:3426
DEV_UI ?= 127.0.0.1:3427
dev:
	@if command -v fswatch >/dev/null; then \
	  trap 'pkill -P $$pid 2>/dev/null; kill $$pid 2>/dev/null; wait $$pid 2>/dev/null; exit 0' INT TERM; \
	  while true; do \
	    $(MAKE) --no-print-directory dev-once & pid=$$!; \
	    fswatch -1 -r -e '.*' -i '\.go$$' -i '\.md$$' . >/dev/null; \
	    echo "  go changed · rebuilding"; pkill -P $$pid 2>/dev/null; kill $$pid 2>/dev/null; wait $$pid 2>/dev/null; \
	  done; \
	else $(MAKE) --no-print-directory dev-once; fi

dev-once:
	@go build -tags dev -o magpie-dev . && echo "  magpie-dev · UI from internal/gui/assets, reload on save · gateway $(DEV_ADDR)"
	@MAGPIE_ADDR=$(DEV_ADDR) MAGPIE_DEV_UI=$(DEV_UI) ./magpie-dev app
