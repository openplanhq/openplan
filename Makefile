# openplan
#
# Run `make` or `make help` for the target list.

SPEC := docs/openapi.yaml

# Points at the Compose Postgres from docker-compose.yaml. Override to run the
# integration tests somewhere else:
#
#   make differential-test TEST_DSN=postgres://…
TEST_DSN ?= postgres://openplan:openplan@localhost:55432/openplan_test?sslmode=disable

# The preview is a static `build-docs` page, not Redocly v1's live-reload
# `preview-docs`: that one loads Redoc from cdn.redocly.com/redoc/latest, which
# now serves an ES-module build its classic <script> tag can't run, so it shows
# a blank page. `build-docs` pins its Redoc version and pre-renders the HTML.
# Re-run the target to see spec edits.
#
# An exact version, not a range like `@2`. npx never counts a range as
# installed: it resolves the range against the registry and prompts to install
# on every run, even when that version is already in its cache. Bump it by
# hand.
REDOCLY := @redocly/cli@2.53.0
API_DOCS_PORT ?= 8080

# Pinned and run through `go run`, like Redocly through npx: no global install,
# and everyone lints with the same version. The first run builds it (~30s);
# later runs hit the build cache. Linters are configured in .golangci.yml.
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

# Pinned the same way. It reports vulnerabilities only on code paths this
# module actually calls, and CI runs this exact command.
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.1.4

.DEFAULT_GOAL := help

.PHONY: help check api-docs-preview api-docs-lint postman-collection lint vulncheck differential-test sandbox-test workflow-replay

help: ## List the available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# What CI runs, in Linux containers, so a change can pass here before it is
# pushed. See scripts/check.sh for what it covers and what it leaves out.
check: ## Run the CI checks locally (needs only Docker)
	scripts/check.sh

api-docs-preview: ## Build and serve the API reference (docs/openapi.yaml)
	npx $(REDOCLY) build-docs $(SPEC) -o tmp/api-docs/index.html
	@echo "API reference at http://127.0.0.1:$(API_DOCS_PORT)"
	python3 -m http.server $(API_DOCS_PORT) --bind 127.0.0.1 --directory tmp/api-docs

api-docs-lint: ## Validate docs/openapi.yaml against the OpenAPI spec
	npx $(REDOCLY) lint $(SPEC)

# The collection is generated, never edited by hand: TestCollectionMatchesSpec
# fails CI when postman/ and the spec disagree, and this is the fix.
postman-collection: ## Regenerate the Postman collection in postman/ from docs/openapi.yaml
	go run ./scripts/postman-collection -spec $(SPEC) -out postman

lint: ## Lint the Go code with golangci-lint
	$(GOLANGCI_LINT) run ./...

vulncheck: ## Scan the Go code for known vulnerabilities it calls
	$(GOVULNCHECK) ./...

# internal/authorization/write.go is a transcription of OpenFGA's own write path
# with its BeginTx/Commit removed, so a tuple write can join our transaction.
# A transcription diverges silently: an upstream migration that adds a column
# keeps compiling here and starts writing rows that are subtly wrong.
#
# differential_test.go is the only thing that catches that, and it needs a real
# database -- without one it skips, and a skipped guard is no guard.
#
# Run it after every `go get github.com/openfga/openfga@...`.
differential-test: ## Run the authorization tests that need a real Postgres
	OPENPLAN_POSTGRES_TEST_DSN=$(TEST_DSN) go test ./internal/authorization/ -count=1 -v

# Temporal replays a running workflow's history against the code the worker has
# now, so a workflow change that alters its commands fails every run in flight
# across an upgrade. This records the histories a release produces, from its own
# checkout, and replays them against this one. REF defaults to the last release
# tag. Needs Compose's Temporal (`docker compose up -d --wait temporal`).
workflow-replay: ## Replay the last release's workflow histories against this checkout
	scripts/workflow-replay.sh $(REF)

sandbox-test: ## Run the sandbox tests (#245) against runsc in the executor image (needs Docker)
	docker build -f Dockerfile.executor -t openplan-executor:sandbox .
	docker build -f internal/sandbox/sandboxtest/Dockerfile --build-arg EXECUTOR_IMAGE=openplan-executor:sandbox -t openplan-sandbox-test .
	docker run --rm --cap-drop ALL --cap-add SYS_ADMIN --cap-add SETUID --cap-add SETGID --cap-add SETFCAP --cap-add CHOWN --cap-add NET_ADMIN --cap-add NET_RAW --sysctl net.ipv4.ip_forward=1 \
		--security-opt no-new-privileges:true --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
		--cgroupns private --tmpfs /var/lib/openplan/runs:exec --tmpfs /var/lib/openplan/sandboxes openplan-sandbox-test
	./internal/sandbox/sandboxtest/failclosed.sh openplan-executor:sandbox
