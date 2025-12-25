.DEFAULT_GOAL:=help

.PHONY: image
image: ## Build the latest container image
	DOCKER_BUILDKIT=1 docker build -t ghcr.io/chaos-mesh/chaos-coredns:latest .

.PHONY: coredns
coredns: image ## Build the coredns executable binary
	docker container create --name extract-coredns ghcr.io/chaos-mesh/chaos-coredns:latest
	docker container cp extract-coredns:/coredns ./coredns
	docker container rm -f extract-coredns

protoc: ## Generate the protobuf code
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	protoc --proto_path=pb --go_out=pb --go_opt=paths=source_relative ./pb/dns.proto

fmt:
	find . -type f -name '*.go' -not -path './pb/**' -exec goimports -w -l {} +

# The help will print out all targets with their descriptions organized bellow their categories. The categories are represented by `##@` and the target descriptions by `##`.
# The awk commands is responsible to read the entire set of makefiles included in this invocation, looking for lines of the file as xyz: ## something, and then pretty-format the target and help. Then, if there's a line with ##@ something, that gets pretty-printed as a category.
# More info over the usage of ANSI control characters for terminal formatting: https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info over awk command: http://linuxcommand.org/lc3_adv_awk.php
.PHONY: help
help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
