FROM golang:1.27.1-alpine AS build
ENV GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=256MiB
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/setup ./cmd/setup && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM alpine:3.22
RUN addgroup -S -g 10001 secureai && adduser -S -D -H -u 10001 -G secureai secureai
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ /app/
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/server"]
