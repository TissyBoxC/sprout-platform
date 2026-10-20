FROM golang:1.27 AS build
ARG RELEASE_VERSION=dev

WORKDIR /src
COPY packages/go/observability/go.mod packages/go/observability/go.sum ./packages/go/observability/
COPY packages/go/httpapi/go.mod packages/go/httpapi/go.sum ./packages/go/httpapi/
COPY services/device_platform/go.mod services/device_platform/go.sum ./services/device_platform/
WORKDIR /src/services/device_platform
RUN go mod download

WORKDIR /src
COPY packages/go ./packages/go
COPY services/device_platform ./services/device_platform
WORKDIR /src/services/device_platform
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=${RELEASE_VERSION}" -o /out/device-platform ./cmd/device-platform
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=${RELEASE_VERSION}" -o /out/device-platform-admin ./cmd/device-platform-admin

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && adduser -D -H -u 65532 sprout
USER 65532:65532
COPY --from=build /out/device-platform /device-platform
COPY --from=build /out/device-platform-admin /device-platform-admin
EXPOSE 8081
ENTRYPOINT ["/device-platform"]
