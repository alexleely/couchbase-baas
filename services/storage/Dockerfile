FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /storage ./cmd/main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /storage .
EXPOSE 8004
CMD ["./storage"]
