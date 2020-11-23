VERSION := `git describe --tags`
BUILDFLAGS := -mod=vendor -ldflags="-s -w -X main.version=$(VERSION)" -gcflags=-trimpath=$(CURDIR)

razvhost:
	go build $(BUILDFLAGS) ./cmd/razvhost

.PHONY: razvhost
