GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
BIN_TERRAPLANE=./bin/terraplane
BIN_TERRAPLANE_LINUX=$(BIN_TERRAPLANE)-linux-amd64
BIN_TERRAPLANE_DARWIN=$(BIN_TERRAPLANE)-darwin-arm64
GOLANGCI_LINT := golangci-lint

.PHONY: build unit-test tests clean generate run-orchestrator run-agent build-linux build-darwin lint

default: build


build:
		CGO_ENABLED=0 $(GOBUILD) -o $(BIN_TERRAPLANE) -v .

unit-test:
		$(GOVET) ./...
		TEST=true $(GOTEST) -v -coverprofile=c.out ./...

tests:
		$(GOVET) ./...
		$(GOTEST) -v --tags=integration -coverprofile=c.out ./...

clean:
		$(GOCLEAN)
		rm -f $(BIN_TERRAPLANE) $(BIN_TERRAPLANE_LINUX)
generate:
		$(GOCMD) generate -v ./...

run-orchestrator: build
		$(BIN_TERRAPLANE) orchestrator

run-agent: build
		$(BIN_TERRAPLANE) agent

protoc-gen:
	protoc -I=./proto \
	  --go_out=. \
	  --go_opt=module=github.com/xyzjace/terraplane \
	  ./proto/terraplane/v1/*.proto

# Cross compilation
build-linux:
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BIN_TERRAPLANE_LINUX) -v .

build-darwin:
		CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BIN_TERRAPLANE_DARWIN) -v .

lint:
	$(GOLANGCI_LINT) run
