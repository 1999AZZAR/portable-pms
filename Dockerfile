FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o pms src/cmd/pms/main.go

FROM alpine:3.18

RUN apk add --no-cache ffmpeg ca-certificates sqlite-libs

RUN addgroup -g 1000 pms && \
    adduser -D -u 1000 -G pms pms

WORKDIR /app

COPY --from=builder /build/pms /app/
COPY --chown=pms:pms web /app/web

USER pms

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/pms"]
CMD ["--path", "/media", "--port", "8080", "--log-level", "info"]
