# Stage 1: Builder
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o wisp .

# Stage 2: Lightweight runtime
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/wisp .
COPY --from=builder /app/wisp.conf .

EXPOSE 8080

CMD ["./wisp"]
