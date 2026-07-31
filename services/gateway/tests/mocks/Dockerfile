# Build stage
FROM docker.io/golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download || true
COPY main.go .
RUN CGO_ENABLED=0 go build -o mock-auth main.go

# Runtime stage
FROM docker.io/alpine:latest
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/mock-auth .
EXPOSE 1080
CMD ["./mock-auth"]
