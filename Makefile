.PHONY: run test fmt vet openapi openapi-check check tidy

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt required"; gofmt -l .; exit 1)

vet:
	go vet ./...

openapi:
	go run ./cmd/openapi

openapi-check:
	go run ./cmd/openapi -check

check: fmt vet test openapi-check

tidy:
	go mod tidy
