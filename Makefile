.PHONY: run test fmt vet check tidy

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt required"; gofmt -l .; exit 1)

vet:
	go vet ./...

check: fmt vet test

tidy:
	go mod tidy
