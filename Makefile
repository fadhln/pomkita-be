.PHONY: run test fmt vet race openapi openapi-check check tidy

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt required"; gofmt -l .; exit 1)

vet:
	go vet ./...

race:
	go test -race ./internal/service/... ./internal/httpapi ./cmd/server

openapi:
	go run ./cmd/openapi

openapi-check:
	go run ./cmd/openapi -check

check: fmt vet test race openapi-check

tidy:
	go mod tidy
