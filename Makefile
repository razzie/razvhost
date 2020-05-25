build:
	go build -mod=vendor -ldflags="-s -w" -gcflags=-trimpath=$(CURDIR) .

.PHONY: build