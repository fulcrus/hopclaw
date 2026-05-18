GO ?= go
CONFIG ?= ./config.example.yaml
STATICCHECK ?= staticcheck
STATICCHECK_CHECKS ?= all,-U1000,-ST1000,-ST1005,-ST1020,-ST1021,-S1011
RACE_PKGS ?= ./agent ./bootstrap ./gateway ./runtime ./server ./store ./toolruntime ./watch ./internal/metrics
COVERAGE_DIR ?= ./.tmp/coverage
COVERAGE_FILE ?= $(COVERAGE_DIR)/coverage.out
COVERAGE_PKGS ?= ./agent ./approval ./eventbus ./gateway ./internal/metrics ./logging ./model ./runtime ./store ./toolruntime
TEST_COVER_GOCACHE ?= $(CURDIR)/.tmp/gocache
COVERAGE_MIN ?= 60.0

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
CHANNEL ?= stable
LDFLAGS = -X github.com/fulcrus/hopclaw/internal/version.Version=$(VERSION) \
          -X github.com/fulcrus/hopclaw/internal/version.Channel=$(CHANNEL) \
          -X github.com/fulcrus/hopclaw/internal/version.GitCommit=$(GIT_COMMIT) \
          -X github.com/fulcrus/hopclaw/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: fmt vet staticcheck staticcheck-u1000 check-repo-hygiene check-worktree-hygiene check test test-race test-cover test-cover-gates bench ci build build-enterprise install run docker docker-push

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

staticcheck:
	$(STATICCHECK) -checks='$(STATICCHECK_CHECKS)' ./...

staticcheck-u1000:
	./scripts/staticcheck_u1000.sh

check-repo-hygiene:
	./scripts/check_repo_hygiene.sh

check-worktree-hygiene:
	CHECK_WORKTREE=1 ./scripts/check_repo_hygiene.sh

check: vet staticcheck staticcheck-u1000 check-repo-hygiene test-race test-cover

test:
	$(GO) test ./...

test-race:
	$(GO) test -race $(RACE_PKGS)

test-cover:
	mkdir -p $(COVERAGE_DIR)
	mkdir -p $(TEST_COVER_GOCACHE)
	rm -f $(COVERAGE_FILE)
	GOCACHE=$(TEST_COVER_GOCACHE) $(GO) test -coverprofile=$(COVERAGE_FILE) $(COVERAGE_PKGS)
	@$(GO) tool cover -func=$(COVERAGE_FILE)
	@coverage="$$( $(GO) tool cover -func=$(COVERAGE_FILE) | awk '/^total:/ { gsub("%","",$$3); print $$3 }' )"; \
		echo "coverage=$$coverage% (minimum $(COVERAGE_MIN)%)"; \
		awk "BEGIN { exit !($$coverage >= $(COVERAGE_MIN)) }" || { echo "coverage gate failed: $$coverage% < $(COVERAGE_MIN)%"; exit 1; }

test-cover-gates:
	./scripts/test_coverage_gates.sh $(COVERAGE_FILE)

bench:
	$(GO) test -bench=. -benchmem -run=^$$ ./model ./toolruntime ./eventbus

ci: check test-cover-gates

build:
	mkdir -p ./bin
	$(GO) build -ldflags "$(LDFLAGS)" -o ./bin/hopclaw ./cmd/hopclaw
	$(GO) build -ldflags "$(LDFLAGS)" -o ./bin/openclaw ./cmd/openclaw
	$(GO) build -ldflags "$(LDFLAGS)" -o ./bin/hopclaw-browserd ./cmd/hopclaw-browserd
	$(GO) build -ldflags "$(LDFLAGS)" -o ./bin/hopclaw-desktopd ./cmd/hopclaw-desktopd

build-enterprise:
	mkdir -p ./bin
	$(GO) build -tags enterprise -ldflags "$(LDFLAGS)" -o ./bin/hopclaw ./cmd/hopclaw
	$(GO) build -tags enterprise -ldflags "$(LDFLAGS)" -o ./bin/openclaw ./cmd/openclaw
	$(GO) build -tags enterprise -ldflags "$(LDFLAGS)" -o ./bin/hopclaw-browserd ./cmd/hopclaw-browserd
	$(GO) build -tags enterprise -ldflags "$(LDFLAGS)" -o ./bin/hopclaw-desktopd ./cmd/hopclaw-desktopd

install:
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/hopclaw
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/openclaw
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/hopclaw-browserd
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/hopclaw-desktopd

run:
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/hopclaw --config $(CONFIG)

DOCKER_IMAGE ?= hopclaw
DOCKER_TAG ?= $(VERSION)

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--build-arg CHANNEL=$(CHANNEL) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest .

docker-push:
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest
