FROM docker.io/library/golang:1.27.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /nadir ./cmd/api

FROM docker.io/library/alpine:3.24
RUN apk add --no-cache curl
WORKDIR /app
COPY --from=builder /nadir /app/nadir
COPY internal/bootstrap/configuration/config.yaml /app/internal/bootstrap/configuration/config.yaml
EXPOSE 8100
ENTRYPOINT ["/app/nadir"]
