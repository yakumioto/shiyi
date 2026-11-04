GO ?= go
BINARY ?= shiyi

.PHONY: build test vet fmt run test-frozen clean

build:
	$(GO) build -o $(BINARY) ./cmd/shiyi

test:
	$(GO) test ./... -race

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

run: build
	./$(BINARY) serve

test-frozen:
	$(GO) test ./... -race -run Frozen

clean:
	rm -f $(BINARY)
