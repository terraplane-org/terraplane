GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOVET=$(GOCMD) vet
BIN_TERRAPLANE_ORCHESTRATOR=./bin/terraplane
BIN_TERRAPLANE_ORCHESTRATOR_LINUX=$(BIN_TERRAPLANE_ORCHESTRATOR)-linux-amd64
BIN_TERRAPLANE_ORCHESTRATOR_DARWIN=$(BIN_TERRAPLANE_ORCHESTRATOR)-darwin-arm64
GOLANGCI_LINT := golangci-lint

.PHONY: build unit-test tests clean generate run build-linux lint

default: build


build:
		CGO_ENABLED=0 $(GOBUILD) -o $(BIN_TERRAPLANE_ORCHESTRATOR) -v .

unit-test:
		$(GOVET) ./...
		TEST=true $(GOTEST) -v -coverprofile=c.out ./...

tests:
		$(GOVET) ./...
		$(GOTEST) -v --tags=integration -coverprofile=c.out ./...

clean:
		$(GOCLEAN)
		rm -f $(BIN_TERRAPLANE_ORCHESTRATOR) $(BIN_TERRAPLANE_ORCHESTRATOR_LINUX)
generate:
		$(GOCMD) generate -v ./...

run: build
		$(BIN_TERRAPLANE_ORCHESTRATOR) serve

# Cross compilation
build-linux:
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BIN_TERRAPLANE_ORCHESTRATOR_LINUX) -v .

build-darwin:
		CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BIN_TERRAPLANE_ORCHESTRATOR_DARWIN) -v .


lint:
	$(GOLANGCI_LINT) run
