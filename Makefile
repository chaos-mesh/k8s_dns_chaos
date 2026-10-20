.DEFAULT_GOAL:=help

.PHONY: image
image: ## Build the latest container image
	DOCKER_BUILDKIT=1 docker build -t ghcr.io/chaos-mesh/chaos-coredns:latest .

.PHONY: coredns
coredns: ## Build the coredns executable binary locally
	CGO_ENABLED=0 go build -o coredns ./cmd/coredns

.PHONY: check
check: ## Run unit tests, compile E2E tests, and check Go code
	go test -race -count=1 ./chaos ./cmd/coredns ./pb
	go test -run '^$$' ./...
	go vet ./...
	go mod tidy -diff

.PHONY: protoc
protoc: ## Generate protobuf and gRPC code with the module-locked tools
	protoc --plugin=protoc-gen-go="$$(go tool -n protoc-gen-go)" \
		--plugin=protoc-gen-go-grpc="$$(go tool -n protoc-gen-go-grpc)" \
		--proto_path=pb --go_out=paths=source_relative:pb \
		--go-grpc_out=paths=source_relative,require_unimplemented_servers=false:pb ./pb/dns_chaos.proto

# The help will print out all targets with their descriptions organized bellow their categories. The categories are represented by `##@` and the target descriptions by `##`.
# The awk commands is responsible to read the entire set of makefiles included in this invocation, looking for lines of the file as xyz: ## something, and then pretty-format the target and help. Then, if there's a line with ##@ something, that gets pretty-printed as a category.
# More info over the usage of ANSI control characters for terminal formatting: https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info over awk command: http://linuxcommand.org/lc3_adv_awk.php
.PHONY: help
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
