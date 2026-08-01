FROM golang:1.24-alpine

WORKDIR /app

COPY mock-not-diamond.go .
EXPOSE 8080

CMD ["go", "run", "mock-not-diamond.go"]
