FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /nadir ./cmd/http

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /nadir /app/nadir
COPY config/config.yaml /app/config/config.yaml
EXPOSE 8080
ENTRYPOINT ["/app/nadir"]
