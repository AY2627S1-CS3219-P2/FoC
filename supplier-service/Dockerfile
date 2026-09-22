# Build context is the repository root (see compose.yaml) so this stage
# can copy the shared seed data in addition to the service's own source.
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY supplier-service/go.mod supplier-service/go.sum ./
RUN go mod download

COPY supplier-service/ .
RUN CGO_ENABLED=0 go build -o /out/supplier-service ./cmd/api

FROM alpine:3.20
WORKDIR /app

COPY --from=builder /out/supplier-service ./supplier-service
COPY supplier-service/migrations ./migrations
COPY data/csv/supplier-seed-data.csv ./data/csv/supplier-seed-data.csv

ENV SEED_CSV_PATH=/app/data/csv/supplier-seed-data.csv
EXPOSE 8082

ENTRYPOINT ["./supplier-service"]
