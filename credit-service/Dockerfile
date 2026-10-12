FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/credit-service ./cmd/api

FROM alpine:3.20

WORKDIR /app

COPY --from=build /out/credit-service /app/credit-service
COPY migrations /app/migrations

EXPOSE 8084

CMD ["/app/credit-service"]
