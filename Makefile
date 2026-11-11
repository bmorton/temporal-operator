# Image URL to use all building/pushing image targets
IMG ?= controller:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ui-generate ## Generate DeepCopy methods and templ Go code.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: gen-version-matrix
gen-version-matrix: ## Regenerate internal/temporal/versions_gen.go from hack/version-matrix.yaml.
	go run hack/gen-version-matrix.go

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: test-golden-update
test-golden-update: ## Regenerate config-template golden files.
	go test ./internal/temporal/ -run TestRenderConfigGolden -update

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= temporal-operator-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: kind ## Set up a Kind cluster for e2e tests if it does not exist
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: preview-wasm
preview-wasm: ## Build the WebAssembly resource-preview tool into docs/static/preview.
	./hack/build-preview.sh

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name temporal-operator-builder
	$(CONTAINER_TOOL) buildx use temporal-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm temporal-operator-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

.PHONY: helm-chart
helm-chart: kubebuilder ## (Re)generate the Helm chart under dist/chart deterministically.
	go run ./hack/helmgen --kubebuilder=$(KUBEBUILDER)

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart (requires helm).
	helm lint dist/chart

.PHONY: bundle
bundle: manifests ## Generate the OLM bundle (requires operator-sdk).
	operator-sdk generate kustomize manifests -q
	"$(KUSTOMIZE)" build config/default | operator-sdk generate bundle \
		--version $(VERSION) --channels stable,alpha --default-channel stable
	operator-sdk bundle validate ./bundle --select-optional name=operatorhub

.PHONY: bundle-build
bundle-build: ## Build the OLM bundle image.
	$(CONTAINER_TOOL) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

VERSION ?= 0.1.0
BUNDLE_IMG ?= ghcr.io/bmorton/temporal-operator-bundle:v$(VERSION)


##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= $(LOCALBIN)/kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
CHAINSAW ?= $(LOCALBIN)/chainsaw
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs
KUBEBUILDER ?= $(LOCALBIN)/kubebuilder
TEMPL ?= $(LOCALBIN)/templ

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.1
CONTROLLER_TOOLS_VERSION ?= v0.19.0
KIND_VERSION ?= v0.27.0
CHAINSAW_VERSION ?= v0.2.15
CRD_REF_DOCS_VERSION ?= v0.3.0
TEMPL_VERSION ?= v0.3.1020

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.5.0
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: chainsaw
chainsaw: $(CHAINSAW) ## Download chainsaw locally if necessary.
$(CHAINSAW): $(LOCALBIN)
	$(call go-install-tool,$(CHAINSAW),github.com/kyverno/chainsaw,$(CHAINSAW_VERSION))

.PHONY: chainsaw-test
chainsaw-test: chainsaw ## Run Chainsaw e2e tests against the current kube context.
	"$(CHAINSAW)" test --test-dir test/e2e --config .chainsaw.yaml

.PHONY: chainsaw-test-nsc
chainsaw-test-nsc: chainsaw ## Run a Chainsaw suite on an ephemeral nsc cluster. Override with SUITE=, NSC_DURATION=, NSC_K8S_VERSION=.
	CHAINSAW="$(CHAINSAW)" ./hack/nsc-e2e.sh

.PHONY: nsc-clean
nsc-clean: ## Destroy ALL nsc clusters labeled app=temporal-operator-e2e (leaves the builder instance untouched).
	@ids="$$(nsc list --all -o json | jq -r '(. // []) | .[] | select(.labels.app == "temporal-operator-e2e") | .cluster_id')"; \
	if [ -z "$$ids" ]; then \
		echo "No temporal-operator-e2e clusters to clean up."; \
	else \
		for id in $$ids; do \
			echo "Destroying $$id"; \
			nsc destroy "$$id" --force; \
		done; \
	fi

.PHONY: azure-e2e-up
azure-e2e-up: chainsaw ## Provision AKS + Flexible Server and install the operator.
	CHAINSAW="$(CHAINSAW)" ./hack/azure-e2e.sh up

.PHONY: azure-e2e-test
azure-e2e-test: chainsaw ## Run the Azure passwordless Chainsaw suite against the standing cluster.
	CHAINSAW="$(CHAINSAW)" ./hack/azure-e2e.sh test

.PHONY: azure-e2e-deploy
azure-e2e-deploy: ## Deploy a standing, usable TemporalCluster against an already-provisioned environment.
	./hack/azure-e2e.sh deploy

.PHONY: azure-e2e-up-deploy
azure-e2e-up-deploy: chainsaw ## Provision everything, then leave a standing, usable TemporalCluster.
	CHAINSAW="$(CHAINSAW)" ./hack/azure-e2e.sh up-deploy

.PHONY: azure-e2e-down
azure-e2e-down: ## Delete the Azure e2e resource group (everything).
	./hack/azure-e2e.sh down

.PHONY: entra-e2e-up
entra-e2e-up: ## Create the longstanding Entra app registration (app roles + client secret).
	./hack/entra-e2e.sh up

.PHONY: entra-e2e-token
entra-e2e-token: ## Print a client-credentials access token for gRPC auth testing.
	./hack/entra-e2e.sh token

.PHONY: entra-e2e-down
entra-e2e-down: ## Delete the Entra app registration created by entra-e2e-up.
	./hack/entra-e2e.sh down

.PHONY: azure-e2e
azure-e2e: chainsaw ## Provision -> test -> teardown in one shot (always tears down).
	CHAINSAW="$(CHAINSAW)" ./hack/azure-e2e.sh all

.PHONY: azure-e2e-clean
azure-e2e-clean: ## Delete ANY resource group tagged app=temporal-operator-e2e (leak backstop).
	./hack/azure-e2e.sh clean

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS) ## Download crd-ref-docs locally if necessary.
$(CRD_REF_DOCS): $(LOCALBIN)
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/elastic/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

.PHONY: templ
templ: $(TEMPL) ## Download templ locally if necessary.
$(TEMPL): $(LOCALBIN)
	$(call go-install-tool,$(TEMPL),github.com/a-h/templ/cmd/templ,$(TEMPL_VERSION))

.PHONY: ui-generate
ui-generate: templ ## Generate Go code from .templ files.
	"$(TEMPL)" generate

.PHONY: api-docs
api-docs: crd-ref-docs ## Generate CRD API reference documentation.
	"$(CRD_REF_DOCS)" --source-path=./api/v1alpha1 --config=hack/crd-ref-docs-config.yaml --renderer=markdown --output-path=docs/api/v1alpha1.md

.PHONY: docs-crd-reference
docs-crd-reference: crd-ref-docs ## Generate the Hugo CRD reference page (docs/content/docs/reference/_index.md).
	@mkdir -p docs/content/docs/reference
	@printf '+++\ntitle = "CRD Reference"\nweight = 70\naliases = ["/reference/"]\n+++\n\n' > docs/content/docs/reference/_index.md
	"$(CRD_REF_DOCS)" --source-path=./api/v1alpha1 --config=hack/crd-ref-docs-config.yaml --renderer=markdown --output-path=docs/content/docs/reference/.crd-reference-body.md
	@cat docs/content/docs/reference/.crd-reference-body.md >> docs/content/docs/reference/_index.md
	@rm -f docs/content/docs/reference/.crd-reference-body.md docs/content/docs/reference/crds.md
	@echo "Generated docs/content/docs/reference/_index.md"

.PHONY: docs-examples
docs-examples: ## Generate the Hugo examples pages from examples/ (git-ignored).
	./hack/build-examples-docs.sh

.PHONY: docs-serve
docs-serve: docs-examples ## Serve the documentation site locally at http://localhost:1313 (requires Hugo Extended).
	hugo server --source docs

.PHONY: docs-build
docs-build: docs-examples ## Build the documentation site into docs/public (requires Hugo Extended).
	hugo --source docs --minify

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: kubebuilder
kubebuilder: $(KUBEBUILDER) ## Build kubebuilder (version pinned in hack/tools/go.mod) into ./bin.
$(KUBEBUILDER): $(LOCALBIN) hack/tools/go.mod
	cd hack/tools && GOBIN="$(LOCALBIN)" go install sigs.k8s.io/kubebuilder/v4

.PHONY: install-tools
install-tools: controller-gen kustomize envtest golangci-lint chainsaw crd-ref-docs kind kubebuilder templ ## Install all pinned developer tooling into ./bin.
	@echo "Developer tooling installed in $(LOCALBIN)."

.PHONY: kind-up
kind-up: kind ## Create a local kind cluster for development.
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) echo "Kind cluster '$(KIND_CLUSTER)' already exists." ;; \
		*) echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; $(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac

.PHONY: kind-down
kind-down: ## Delete the local kind cluster.
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

.PHONY: kind-load
kind-load: ## Load the operator image into the local kind cluster.
	$(KIND) load docker-image ${IMG} --name $(KIND_CLUSTER)

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
