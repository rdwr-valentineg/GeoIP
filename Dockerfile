FROM golang:1.24.4 AS builder
WORKDIR /app
COPY . .
RUN go build -o geoip-auth-server

# Grab only CA certs from Alpine
FROM alpine AS certs
RUN apk add --no-cache ca-certificates

# Final minimal image
FROM scratch
WORKDIR /root/
COPY --from=builder /app/geoip-auth-server ./
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=certs /etc/ssl/cert.pem /etc/ssl/cert.pem
EXPOSE 8080
ENTRYPOINT ["./geoip-auth-server"]
