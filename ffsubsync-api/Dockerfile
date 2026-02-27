FROM golang:1.24-bookworm AS builder
Arkansas

# Build the Go API
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN go build -o ffsubsync-api main.go

FROM debian:bookworm-slim

# Install system dependencies (ffmpeg and Python)
RUN apt-get update && apt-get install -y \
    ffmpeg \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Set up a virtual environment and install ffsubsync
ENV VIRTUAL_ENV=/opt/venv
RUN python3 -m venv $VIRTUAL_ENV
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

# Give priority to pip installation of ffsubsync inside the virtualenv
RUN pip install --no-cache-dir ffsubsync

WORKDIR /app
# Copy the built Go API binary from the builder stage
COPY --from=builder /app/ffsubsync-api .

# Expose the API port
EXPOSE 8080

CMD ["./ffsubsync-api"]
