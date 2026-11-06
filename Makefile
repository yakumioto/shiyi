GO ?= go
BINARY ?= shiyi
IMAGE ?= shiyi:latest

# The Docker CLI writes its state under ~/.docker, which is not writable in
# every sandbox. Point it at a throwaway directory outside the repository, so
# a build never writes into the working tree. Override to reuse a real config.
DOCKER_CONFIG ?= $(or $(TMPDIR),/tmp)/shiyi-docker-config

.PHONY: check prereqs browser-prereqs browser-check verify-vendor vet compile build test test-frozen fmt run smoke readme-check image image-check clean

# check is the one local gate. CI invokes the same target, including the real
# HTTPS browser acceptance, authenticated process smoke and docs consistency.
#
# A green `make check` is what "the gate is green" means in this repository.
# Run it before claiming a change is done.
check: prereqs verify-vendor vet compile test test-frozen browser-check smoke readme-check
	@echo
	@echo "make check: ALL GREEN"

# prereqs fails early with a readable message when a tool the tests shell out
# to is missing, instead of letting it surface as a confusing test failure.
prereqs: browser-prereqs
	@for tool in rg python3 openssl; do \
		command -v $$tool >/dev/null 2>&1 || { \
			echo "make: $$tool is required for search / authenticated TLS smoke" >&2; exit 1; \
		}; \
	done

# These are test prerequisites, not runtime/production configuration.
browser-prereqs:
	@bash scripts/browser-prereqs.sh

browser-check: browser-prereqs
	@echo "== real HTTPS browser acceptance =="
	node --test tests/browser/web-auth.test.mjs

# verify-vendor catches a go.mod/go.sum change that was not followed by
# `go mod vendor`, which would make the container build fail.
verify-vendor:
	@echo "== vendor consistency =="
	$(GO) mod verify
	$(GO) mod vendor
	git diff --exit-code -- vendor/

vet:
	@echo "== vet =="
	$(GO) vet ./...

compile:
	@echo "== compile every package =="
	$(GO) build ./...

build:
	$(GO) build -o $(BINARY) ./cmd/shiyi

test:
	@echo "== test -race =="
	$(GO) test ./... -race -count=1

# test-frozen runs the versioned, immutable fixture set on its own. It is
# already part of `test`; this target exists to run it in isolation.
test-frozen:
	@echo "== frozen test set =="
	$(GO) test ./... -race -count=1 -run Frozen

# smoke exercises the real binary against a fake model: it builds its own
# binary into a throwaway directory and needs no network and no API key.
smoke:
	@echo "== end-to-end smoke =="
	bash scripts/smoke.sh

# readme-check cross-checks README, the Dockerfile, the example configuration,
# the error codes and the CLI subcommands, so they cannot drift apart.
readme-check:
	@echo "== docs consistency =="
	bash docs/check-readme.sh

fmt:
	$(GO) fmt ./...

# image-check runs the same authenticated TLS/TTY container smoke as CI.
# Its throwaway containers and named volumes never touch a real deployment.
image:
	@mkdir -p $(DOCKER_CONFIG)
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker build -t $(IMAGE) .

image-check:
	@mkdir -p $(DOCKER_CONFIG)
	@echo "== container image =="
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker build -t $(IMAGE) .
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker run --rm --entrypoint rg $(IMAGE) --version
	DOCKER_CONFIG=$(DOCKER_CONFIG) bash scripts/smoke.sh --image $(IMAGE)

run: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY)
