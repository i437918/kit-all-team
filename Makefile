.PHONY: fmt vet test race build verify

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

build:
	go build ./cmd/teamkit

verify: fmt vet test
