FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /data ./cmd/main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /data .
EXPOSE 8002
CMD ["./data"]
