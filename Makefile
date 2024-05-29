.PHONY: test lint fmt dispatch clean image push

BUILD = build/$(GOOS)/$(GOARCH)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GO ?= go

DOCKER ?= docker
TAG ?= $(shell git log --pretty=format:'%h' -n 1)
REGISTRY ?= 714918108619.dkr.ecr.us-west-2.amazonaws.com
DISPATCH = $(BUILD)/dispatch
IMAGE = $(REGISTRY)/dispatch:$(TAG)

test: dispatch
	$(GO) test ./...

lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...

dispatch:
	$(GO) build -o $(DISPATCH) .

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
