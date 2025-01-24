FROM golang:1.23.1 AS builder

WORKDIR /app

# Copy source code
COPY . .

RUN go mod download

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o backup

# Final stage
FROM alpine:3.18

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/backup .

# Create directory for data
RUN mkdir -p /data

# Set environment variables
ENV TARGET_DATADIR="/data"

VOLUME /data

ENTRYPOINT ["/app/backup"]
