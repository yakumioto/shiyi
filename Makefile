GO ?= go
BINARY ?= shiyi
IMAGE ?= shiyi:latest

# The Docker CLI writes its state under ~/.docker, which is not writable in
# every sandbox. Point it at a throwaway directory outside the repository, so
# a build never writes into the working tree. Override to reuse a real config.
DOCKER_CONFIG ?= $(or $(TMPDIR),/tmp)/shiyi-docker-config

.PHONY: check prereqs verify-vendor vet compile build test test-frozen fmt run smoke readme-check image image-check clean

# check is the one local gate. Its steps are the steps of the test job in
# .github/workflows/ci.yml, in the same order, plus the two repository checks
# that job does not run (the end-to-end smoke and the docs consistency check).
#
# A green `make check` is what "the gate is green" means in this repository.
# Run it before claiming a change is done.
check: prereqs verify-vendor vet compile test test-frozen smoke readme-check
	@echo
	@echo "make check: ALL GREEN"

# prereqs fails early with a readable message when a tool the tests shell out
# to is missing, instead of letting it surface as a confusing test failure.
prereqs:
	@command -v rg >/dev/null 2>&1 || { \
		echo "make: ripgrep (rg) is required — the search and its tests shell out to it" >&2; \
		exit 1; \
	}

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

# image builds the container image; image-check is the container job of CI run
# locally: the image builds, ripgrep is in it, and /healthz answers in it.
image:
	@mkdir -p $(DOCKER_CONFIG)
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker build -t $(IMAGE) .

image-check:
	@mkdir -p $(DOCKER_CONFIG)
	@echo "== container image =="
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker build -t $(IMAGE) .
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker run --rm --entrypoint rg $(IMAGE) --version
	@set -e; \
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker run -d --name shiyi-image-check -p 8788:8787 \
		-e SHIYI_API_KEY=dummy $(IMAGE) >/dev/null; \
	trap 'DOCKER_CONFIG=$(DOCKER_CONFIG) docker rm -f shiyi-image-check >/dev/null 2>&1' EXIT; \
	for i in $$(seq 1 30); do \
		if curl -fsS http://127.0.0.1:8788/healthz >/dev/null 2>&1; then \
			echo "container /healthz ok"; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "container /healthz never answered" >&2; \
	DOCKER_CONFIG=$(DOCKER_CONFIG) docker logs shiyi-image-check >&2; exit 1

run: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY)
