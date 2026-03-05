# Go binaries
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o api ./cmd/api/main.go
RUN go build -o worker ./cmd/worker/main.go


# Final lightweight image
FROM alpine:latest
RUN apk add --no-cache ffmpeg ca-certificates
WORKDIR /root/


# Copying binaries from the builder stage
COPY --from=builder /app/api .
COPY --from=builder /app/worker .


# Expose the API port
RUN mkdir data
EXPOSE 8080

