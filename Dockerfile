# Multi-stage, cross-compiled; standard library only. Templates, the stylesheet and the font are
# embedded in the binary.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/openverse ./cmd/openverse

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 65532 -h /app app
COPY --from=build /out/openverse /app/openverse
USER 65532:65532
ENV OPENVERSE_ADDR=:20195
EXPOSE 20195
ENTRYPOINT ["/app/openverse"]
