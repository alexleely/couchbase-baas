FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /auth ./cmd/main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /auth .
EXPOSE 8001
CMD ["./auth"]
