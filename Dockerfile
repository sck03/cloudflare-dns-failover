# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/cfguard .

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates tzdata iputils && mkdir -p /app/config /app/instance
WORKDIR /app
COPY --from=builder /out/cfguard ./cfguard
ENV TZ=Asia/Shanghai
EXPOSE 8099
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["./cfguard", "-healthcheck"]
ENTRYPOINT ["./cfguard"]
