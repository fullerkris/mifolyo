FROM golang:1.25.13-alpine AS builder

# Set the working directory
WORKDIR /app

# Copy Go modules and files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the application
COPY . .

# Run tests
RUN go test -v ./...

# Build the application
RUN go build -o page-rank ./cmd/page-rank

# Use a smaller base image
FROM alpine:3.22

RUN apk add --no-cache ca-certificates

# Set the working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/page-rank .

USER 65534:65534

CMD ["./page-rank"]
