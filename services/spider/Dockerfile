FROM golang:1.25.13-alpine AS builder

# Set the working directory
WORKDIR /app/services/spider

# Copy Go modules and files
COPY services/spider/go.mod services/spider/go.sum ./

# Download dependencies
RUN go mod download

# Copy the application
COPY services/spider/ ./
# Preserve repository-relative paths used by the conformance tests.
COPY contracts/crawl-jobs-v2/digest-vectors.json /app/contracts/crawl-jobs-v2/digest-vectors.json
COPY contracts/url-canonicalization/v1.json /app/contracts/url-canonicalization/v1.json
COPY docs/crawl-jobs-v2.md /app/docs/crawl-jobs-v2.md
COPY seeds/manual-seeds.csv /app/seeds/manual-seeds.csv

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
COPY --from=builder /app/services/spider/spider .
COPY --from=builder /app/services/spider/config ./config

USER 65534:65534

# EXPOSE ports?
# EXPOSE 8080

# Run the application
CMD ["./spider"]
