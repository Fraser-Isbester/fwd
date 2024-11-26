FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o fwd cmd/fwd/main.go

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/fwd .
ENTRYPOINT ["/app/fwd"]
