.DEFAULT_GOAL := razvhost
BUILDFLAGS := -mod=vendor -ldflags="-s -w" -gcflags=-trimpath=$(CURDIR)

razvhost:
	go build $(BUILDFLAGS) ./cmd/razvhost

.PHONY: razvhost
