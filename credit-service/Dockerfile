# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/credit-service ./cmd/api

# Runtime stage
FROM alpine:3.22

RUN adduser -D -H appuser

WORKDIR /app

COPY --from=builder /out/credit-service ./credit-service
COPY migrations ./migrations

USER appuser

EXPOSE 8084

ENTRYPOINT ["./credit-service"]
