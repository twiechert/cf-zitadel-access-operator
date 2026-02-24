img := "ghcr.io/twiechert/zitadel-access-operator:latest"

controller-gen := "go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest"

# Build the operator binary
build: fmt vet
    go build -o bin/zitadel-access-operator ./cmd/

# Generate deepcopy methods
generate:
    {{ controller-gen }} object paths="./api/..."

# Generate CRD and RBAC manifests
manifests:
    {{ controller-gen }} crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
    {{ controller-gen }} rbac:roleName=zitadel-access-operator paths="./internal/controller/..." output:rbac:artifacts:config=config/rbac

# Format code
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run tests
test:
    go test ./... -coverprofile cover.out

# Build Docker image
docker-build:
    docker build -t {{ img }} .

# Push Docker image
docker-push:
    docker push {{ img }}

# Install CRDs into the cluster
install: manifests
    kubectl apply -f config/crd/bases/
