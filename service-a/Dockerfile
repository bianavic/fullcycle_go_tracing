FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS build

ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src

COPY service-a/go.mod service-a/go.sum ./
RUN go mod download

COPY service-a/ ./
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/service-a ./cmd/server

FROM alpine:3.24

RUN addgroup -S app && adduser -S -G app -H -s /sbin/nologin app
COPY --from=build /out/service-a /usr/local/bin/service-a

USER app
# PORT overrides the listen port; 8080 is the default.
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["service-a"]