NTFY_VERSION := $(shell awk '{ for (i = 1; i < NF; i++) if ($$i == "heckel.io/ntfy/v2" && $$(i + 1) ~ /^v/) { print $$(i + 1); exit } }' go.mod)
NTFY_DIR := third_party/ntfy
NTFY_STAMP := $(NTFY_DIR)/.tossling-$(NTFY_VERSION)
TAGS := sqlite_omit_load_extension,osusergo,netgo
VERSION := $(shell cat VERSION)

.PHONY: build deps vet test smoke browser clean

build: $(NTFY_STAMP)
	CGO_ENABLED=1 go build -tags $(TAGS) -ldflags "-s -w -X main.version=$(VERSION) $(EXTRA_LDFLAGS)" -o build/tossling-server .

deps: $(NTFY_STAMP)

vet: $(NTFY_STAMP)
	go vet -tags $(TAGS) .

test: $(NTFY_STAMP)
	CGO_ENABLED=1 go test -tags $(TAGS) -count=1 .

smoke: build
	@dir=$$(mktemp -d); port=$$((20000 + RANDOM % 20000)); \
	./build/tossling-server -listen 127.0.0.1:$$port -data "$$dir/data" -base-url http://127.0.0.1:$$port > "$$dir/log" 2>&1 & pid=$$!; \
	for i in $$(seq 1 50); do curl -sf http://127.0.0.1:$$port/v1/tossling/health >/dev/null && break; sleep 0.1; done; \
	TOSSLING_CLI="env TOSSLING_DATA=$$dir/data TOSSLING_BASE_URL=http://127.0.0.1:$$port TOSSLING_LISTEN=127.0.0.1:$$port ./build/tossling-server" \
	tests/smoke.sh http://127.0.0.1:$$port "$$dir/data/tossling-server.json"; rc=$$?; \
	link=$$(./build/tossling-server setup-link -data "$$dir/data" -base-url http://127.0.0.1:$$port); \
	code=$$(curl -s -o /dev/null -w '%{http_code}' "$$link"); \
	if [ "$$code" = 200 ]; then echo "ok   setup-link gives a working link"; else echo "FAIL setup-link: $$code"; rc=1; fi; \
	kill $$pid; wait $$pid 2>/dev/null; [ $$rc -eq 0 ] || cat "$$dir/log"; rm -rf "$$dir"; exit $$rc

$(NTFY_STAMP):
	rm -rf $(NTFY_DIR)
	mkdir -p third_party
	src=$$(cd "$${TMPDIR:-/tmp}" && GOFLAGS= go mod download -json heckel.io/ntfy/v2@$(NTFY_VERSION) | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$$/\1/p') && cp -R "$$src" $(NTFY_DIR)
	chmod -R u+w $(NTFY_DIR)
	mkdir -p $(NTFY_DIR)/server/docs $(NTFY_DIR)/server/site
	touch $(NTFY_DIR)/server/docs/index.html $(NTFY_DIR)/server/site/app.html $@

CHROME ?= $(shell command -v google-chrome || command -v chromium || echo "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")

browser: build
	@dir=$$(mktemp -d); port=$$((20000 + RANDOM % 20000)); \
	./build/tossling-server -listen 127.0.0.1:$$port -data "$$dir/data" -base-url http://127.0.0.1:$$port > "$$dir/log" 2>&1 & pid=$$!; \
	for i in $$(seq 1 50); do curl -sf http://127.0.0.1:$$port/v1/tossling/health >/dev/null && break; sleep 0.1; done; \
	secret=$$(sed -n 's/.*"setup_secret": *"\([^"]*\)".*/\1/p' "$$dir/data/tossling-server.json"); \
	node tests/browser.mjs "$(CHROME)" "http://127.0.0.1:$$port/setup/$$secret"; rc=$$?; \
	kill $$pid; wait $$pid 2>/dev/null; [ $$rc -eq 0 ] || cat "$$dir/log"; rm -rf "$$dir"; exit $$rc

clean:
	rm -rf build third_party
