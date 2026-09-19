# Build context is the repository root, matching the other services.
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY api-gateway/go.mod ./
RUN go mod download

COPY api-gateway/ .
RUN CGO_ENABLED=0 go build -o /out/api-gateway ./cmd/api

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/api-gateway ./api-gateway

# Provisional port (D-018).
EXPOSE 8080

ENTRYPOINT ["./api-gateway"]
