FROM golang:1.25.13-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy Go modules and files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the application
COPY . .

# Build the application
RUN go build -o spider ./cmd/spider/main.go

# Run tests
RUN go test -v ./...

# Use a smaller base image
FROM alpine:3.24.1

RUN apk add --no-cache ca-certificates \
    && mkdir -p /run/mifolyo-render \
    && chown 65534:65534 /run/mifolyo-render

# Set the working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/spider .
COPY --from=builder /app/config ./config

USER 65534:65534

# EXPOSE ports?
# EXPOSE 8080

# Run the application
CMD ["./spider"]
