# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY server/go.mod server/go.sum ./
RUN go mod download

# Copy source code
COPY server/ .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o arrgo .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/arrgo .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

EXPOSE 5003

CMD ["./arrgo"]

