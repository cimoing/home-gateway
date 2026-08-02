APP      := home-gateway
CMD      := ./cmd/server
BIN_DIR  := bin
WEB_DIR  := web
DIST_DIR := $(WEB_DIR)/dist
WEBUI_DIST := internal/webui/dist
RELEASE_DIR := dist

GO           ?= go
NPM          ?= npm
CGO_ENABLED  ?= 0
DOCKER       ?= docker
IMAGE        ?= $(APP):local
GOOS         ?= $(shell $(GO) env GOOS)
GOARCH       ?= $(shell $(GO) env GOARCH)

ifeq ($(OS),Windows_NT)
EXE := .exe
else
EXE :=
endif

BINARY := $(BIN_DIR)/$(APP)$(EXE)

.PHONY: all build server web web-deps embed-web test clean run docker release release-amd64 release-arm64 package help

## build web assets, embed them, and compile the Go server (default)
all: build

build: embed-web server

## compile the Go server into bin/ (uses currently embedded web assets)
server: $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build \
		-trimpath \
		-ldflags="-s -w" \
		-o $(BINARY) \
		$(CMD)

## install frontend deps if needed, then build Vue assets
web: web-deps
	$(NPM) --prefix $(WEB_DIR) run build

web-deps:
	@if [ ! -d "$(WEB_DIR)/node_modules" ]; then \
		$(NPM) --prefix $(WEB_DIR) ci; \
	fi

## copy built web assets into the embed package
embed-web: web
	rm -rf $(WEBUI_DIST)
	mkdir -p $(WEBUI_DIST)
	cp -a $(DIST_DIR)/. $(WEBUI_DIST)/

## run Go tests and ensure the frontend still builds
test:
	$(GO) test ./... -count=1
	@$(MAKE) web

## remove build outputs
clean:
	$(GO) clean
	rm -rf $(BIN_DIR) $(DIST_DIR) $(RELEASE_DIR)
	git checkout -- $(WEBUI_DIST) >/dev/null 2>&1 || true

## build and run the local binary (frontend is embedded)
run: build
	DATA=$${DATA:-./data} $(BINARY) run

## build the production Docker image
docker:
	$(DOCKER) build -t $(IMAGE) .

## build linux/amd64 and linux/arm64 release archives
release: embed-web release-amd64 release-arm64

release-amd64:
	@$(MAKE) package GOOS=linux GOARCH=amd64

release-arm64:
	@$(MAKE) package GOOS=linux GOARCH=arm64

package: $(BIN_DIR)
	@set -e; \
	name="$(APP)-$(GOOS)-$(GOARCH)"; \
	outdir="$(RELEASE_DIR)/$${name}"; \
	mkdir -p "$${outdir}"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build \
		-trimpath \
		-ldflags="-s -w" \
		-o "$${outdir}/$(APP)" \
		$(CMD); \
	cp config.example.yaml "$${outdir}/"; \
	(cd $(RELEASE_DIR) && zip -r "$${name}.zip" "$${name}"); \
	echo "built $(RELEASE_DIR)/$${name}.zip"

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

help:
	@echo "Targets:"
	@echo "  make / make build   Build frontend, embed it, and compile server"
	@echo "  make server         Build Go binary only -> $(BINARY)"
	@echo "  make web            Build Vue assets -> $(DIST_DIR)"
	@echo "  make embed-web      Copy web/dist into internal/webui/dist"
	@echo "  make test           Run Go tests and frontend build"
	@echo "  make run            Build and start the server"
	@echo "  make docker         Build Docker image $(IMAGE)"
	@echo "  make release        Build linux/amd64 + linux/arm64 archives"
	@echo "  make clean          Remove bin/, web/dist/, and dist/"
