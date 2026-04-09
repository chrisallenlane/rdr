# Build stage
FROM golang:1.25-alpine AS builder
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /bin/rdr ./cmd/rdr

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/rdr /usr/local/bin/rdr
RUN adduser -D -H rdr && mkdir -p /data && chown rdr:rdr /data
USER rdr
VOLUME /data
ENV RDR_DATA_PATH=/data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO /dev/null http://localhost:8080/login || exit 1
ENTRYPOINT ["rdr"]
