.PHONY: test lint fmt dispatch clean image push

BUILD = build/$(GOOS)/$(GOARCH)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOEXE ?= $(shell go env GOEXE)
GO ?= go

DOCKER ?= docker
TAG ?= $(shell git log --pretty=format:'%h' -n 1)
REGISTRY ?= 714918108619.dkr.ecr.us-west-2.amazonaws.com
DISPATCH = $(BUILD)/dispatch$(GOEXE)
IMAGE = $(REGISTRY)/dispatch:$(TAG)

test: dispatch
	$(GO) test ./...

test-cover: dispatch
	$(GO) test -cover ./...

lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...

dispatch:
	@echo "Building dispatch binary..."
	$(eval VERSION := $(shell git describe --tags --abbrev=0 | cut -c 2-))
	$(eval COMMIT := $(shell git rev-parse --short=8 HEAD))
	$(GO) build -ldflags "-X main.Version=$(VERSION) -X main.Revision=$(COMMIT)" -o $(DISPATCH) .
clean:
	rm -rf ./build

image:
	$(DOCKER) build -t $(IMAGE) .

push: image
	$(DOCKER) push $(IMAGE)

update:
	buf mod update ./proto
	for ref in $$(yq -r '.deps[] | .remote + "/gen/go/" + .owner + "/" + .repository + "/protocolbuffers/go@" + .commit' proto/buf.lock); do go get $$ref; done
	go mod tidy
