FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/pomkita-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pomkita-server /pomkita-server
EXPOSE 8080
ENTRYPOINT ["/pomkita-server"]
