ARG GO_VERSION=1.26.1
FROM golang:${GO_VERSION}-bookworm AS builder

# Build the Go API
WORKDIR /app
COPY shared/ ./shared/
COPY ffsubsync-api/go.mod ./
COPY ffsubsync-api/go.sum* ./

# Update replace directive for Docker build context
RUN sed -i 's|=> ../shared|=> ./shared|g' go.mod

RUN go mod download
COPY ffsubsync-api/main.go ./
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
RUN --mount=type=cache,target=/root/.cache/pip pip install ffsubsync==0.4.26

WORKDIR /app
# Copy the built Go API binary from the builder stage
COPY --from=builder /app/ffsubsync-api .

# Expose the API port
EXPOSE 8080

CMD ["./ffsubsync-api"]
