FROM golang:1.27 AS build
ARG RELEASE_VERSION=dev

WORKDIR /src
COPY packages/go/observability/go.mod packages/go/observability/go.sum ./packages/go/observability/
COPY packages/go/httpapi/go.mod packages/go/httpapi/go.sum ./packages/go/httpapi/
COPY services/voice_gateway/go.mod services/voice_gateway/go.sum ./services/voice_gateway/
WORKDIR /src/services/voice_gateway
RUN go mod download

WORKDIR /src
COPY packages/go ./packages/go
COPY services/voice_gateway ./services/voice_gateway
WORKDIR /src/services/voice_gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildVersion=${RELEASE_VERSION}" -o /out/voice-gateway ./cmd/voice-gateway

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && adduser -D -H -u 65532 sprout
USER 65532:65532
COPY --from=build /out/voice-gateway /voice-gateway
EXPOSE 8082
ENTRYPOINT ["/voice-gateway"]
