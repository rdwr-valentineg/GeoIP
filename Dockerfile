# Multi-stage build
FROM golang:1.24.4 AS builder
WORKDIR /app
COPY . .
RUN go build -o geoip-auth-server

# Final minimal image
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=builder /app/geoip-auth-server ./
EXPOSE 8080
ENTRYPOINT ["./geoip-auth-server"]
