# Image URL to use all building/pushing image targets
IMG ?= controller:latest

GITHUB_PAT_PATH ?=
ifeq (,$(GITHUB_PAT_PATH))
GITHUB_PAT_MOUNT ?=
else
GITHUB_PAT_MOUNT ?= --secret id=github_pat,src=$(GITHUB_PAT_PATH)
endif

.PHONY: target/fedhcp

all: target/fedhcp

target/fedhcp:
	mkdir -p target
	CGO_ENABLED=0 go build -o target/fedhcp .

clean:
	rm -rf target

run: all
	sudo ./target/fedhcp

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} $(GITHUB_PAT_MOUNT) .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: addlicense
addlicense: ## Add license headers to all go files.
	find . -name '*.go' -exec go run github.com/google/addlicense -f hack/license-header.txt {} +

.PHONY: checklicense
checklicense: ## Check that every file has a license header present.
	find . -name '*.go' -exec go run github.com/google/addlicense  -check -c 'OnMetal authors' {} +

lint: ## Run golangci-lint against code.
	golangci-lint run ./...

.PHONY: test
test: fmt vet ## Run tests.
	go test ./... -coverprofile cover.out
