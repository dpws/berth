GO      ?= go
BIN     ?= berth
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS ?= -X main.version=$(VERSION)

# Where `make install` puts the binary. Override for a system-wide install:
#   sudo make install PREFIX=/usr/local
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install uninstall test vet fmt run clean clipd clipd-windows clipd-windows-gui clipd-darwin bundle-windows

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN) .

# berth-clipd runs on the machine whose clipboard you want, not here.
# Build it for that machine and copy the result over.
clipd:
	$(GO) build -ldflags "$(LDFLAGS)" -o dist/berth-clipd ./cmd/berth-clipd

clipd-windows:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" \
		-o dist/berth-clipd.exe ./cmd/berth-clipd

# Same binary with no console window, for dropping in the Startup folder.
# Errors go nowhere, so get it working with the console build first.
clipd-windows-gui:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "-H=windowsgui $(LDFLAGS)" \
		-o dist/berth-clipd-silent.exe ./cmd/berth-clipd

clipd-darwin:
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" \
		-o dist/berth-clipd-darwin ./cmd/berth-clipd

# Everything needed on a Windows machine, ready to copy across.
bundle-windows: clipd-windows clipd-windows-gui
	mkdir -p dist/windows
	cp dist/berth-clipd.exe dist/berth-clipd-silent.exe dist/windows/
	cp install.ps1 dist/windows/
	cp cmd/berth-clipd/README.md dist/windows/
	@command -v zip >/dev/null && cd dist && zip -qr berth-clipd-windows.zip windows || true
	@echo "bundle ready: dist/windows/ (copy it to the Windows machine)"

install: build
	install -d $(BINDIR)
	install -m 0755 $(BIN) $(BINDIR)/$(BIN)
	@echo "installed $(BINDIR)/$(BIN)"
	@command -v $(BIN) >/dev/null || \
		echo "note: $(BINDIR) is not on your PATH yet - open a new shell, or add it"

uninstall:
	rm -f $(BINDIR)/$(BIN)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

run: build
	./$(BIN)

clean:
	rm -f $(BIN)
	rm -rf dist
