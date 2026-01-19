# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go mod
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o indexer .

# Final stage
FROM alpine:3.20

# Add ca-certificates for API calls and tzdata for correct timezones
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/indexer .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

EXPOSE 5004

CMD ["./indexer"]
