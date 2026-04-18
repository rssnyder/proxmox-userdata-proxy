FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /proxy ./cmd/proxy

# Final minimal image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 proxy
USER proxy

COPY --from=builder /proxy /proxy

EXPOSE 8443

ENTRYPOINT ["/proxy"]
