FROM golang:1.22-alpine AS builder

WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o local-proxy .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/local-proxy /usr/local/bin/local-proxy

EXPOSE 8081
CMD ["/usr/local/bin/local-proxy"]
