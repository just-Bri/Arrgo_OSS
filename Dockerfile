# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install git for go mod
RUN apk add --no-cache git

# Copy shared library first (needed for server)
COPY shared/ ./shared/

# Copy go mod files
COPY server/go.mod ./
COPY server/go.sum* ./

# Update replace directive for Docker build context
RUN sed -i 's|=> ../shared|=> ./shared|g' go.mod

RUN go mod download

# Cache breaker for Unraid/SMB environments
ARG BUILD_VERSION=1

# Copy source code
COPY server/ .

# Update replace directive again (after copying source which overwrites go.mod)
RUN sed -i 's|=> ../shared|=> ./shared|g' go.mod

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o arrgo .

# Final stage
FROM alpine:3.20

# Add ca-certificates for API calls and tzdata for correct timezones
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/arrgo .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

EXPOSE 5003

CMD ["./arrgo"]
