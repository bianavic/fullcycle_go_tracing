FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS build

# Each service is its own module; ignore the repository-level go.work.
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src

COPY service-b/go.mod service-b/go.sum ./
RUN go mod download

COPY service-b/ ./
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/service-b ./cmd/server

FROM alpine:3.24

RUN addgroup -S app && adduser -S -G app -H -s /sbin/nologin app
COPY --from=build /out/service-b /usr/local/bin/service-b

USER app
# PORT is injected by Cloud Run; 8081 is the local default.
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider "http://127.0.0.1:${PORT:-8081}/healthz" || exit 1

ENTRYPOINT ["service-b"]
