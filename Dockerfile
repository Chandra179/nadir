FROM golang:1.27.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /nadir ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache curl
WORKDIR /app
COPY --from=builder /nadir /app/nadir
COPY config/config.yaml /app/config/config.yaml
COPY samples /app/samples
EXPOSE 8100
ENTRYPOINT ["/app/nadir"]
