VERSION := `git describe --tags`
BUILDFLAGS := -mod=vendor -ldflags="-s -w -X main.version=$(VERSION)" -gcflags=-trimpath=$(CURDIR)
IMAGE_NAME := razvhost
IMAGE_REGISTRY ?= ghcr.io/razzie
FULL_IMAGE_NAME := $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(VERSION)

.PHONY: build
build:
	go build $(BUILDFLAGS) ./cmd/razvhost

.PHONY: docker-build
docker-build:
	docker build . -t $(FULL_IMAGE_NAME)

.PHONY: docker-push
docker-push: docker-build
	docker push $(FULL_IMAGE_NAME)
