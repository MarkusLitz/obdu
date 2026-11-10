.PHONY: all build test race install dist clean

VERSION := 0.1.0
BINARY := bin/obdu
INSTALL_DIR := $(HOME)/.local/bin
PLATFORMS := linux/amd64 linux/arm64 darwin/arm64 darwin/amd64

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/obdu

test:
	CGO_ENABLED=0 go test -v ./...

race:
	go test -v -race ./...

install: build
	@mkdir -p $(INSTALL_DIR)
	ln -sfn "$(CURDIR)/$(BINARY)" "$(INSTALL_DIR)/obdu"
	@echo "Installed symlink: $(INSTALL_DIR)/obdu -> $(CURDIR)/$(BINARY)"

dist:
	@rm -rf dist
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		stage="dist/obdu_$(VERSION)_$${os}_$${arch}"; \
		mkdir -p "$$stage"; \
		echo "Building $$os/$$arch..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" -o "$$stage/obdu" ./cmd/obdu; \
		cp README.md LICENSE "$$stage/"; \
		tar -C dist -czf "dist/obdu_$(VERSION)_$${os}_$${arch}.tar.gz" "obdu_$(VERSION)_$${os}_$${arch}"; \
		rm -rf "$$stage"; \
	done
	@cd dist && sha256sum *.tar.gz > checksums.txt
	@ls -lh dist/

clean:
	rm -rf bin dist
