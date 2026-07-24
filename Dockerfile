FROM --platform=$BUILDPLATFORM node:22-alpine AS css

WORKDIR /app

COPY package.json ./
RUN npm install

COPY assets ./assets
COPY web ./web

RUN npm run build:css

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod ./
COPY . .

COPY --from=css /app/web/static/app.css ./web/static/app.css

# go.sum isn't committed to the repo (see README), so it's generated here,
# with the full source present, rather than relying on an early
# go-mod-only layer whose cache can go stale without anything else in the
# Dockerfile changing to invalidate it.
RUN go mod tidy

# css and build both stay pinned to --platform=$BUILDPLATFORM above — neither
# needs to run under target-platform emulation at all. Compiled CSS isn't
# architecture-specific in the first place, and Go cross-compiles natively:
# producing an arm64 binary doesn't require the compiler itself to execute
# under arm64 emulation, only GOARCH needs to say so. Only this final stage
# is genuinely platform-specific, and it needs zero execution to become
# so — just a file copy into the matching base image per target.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -o /pollinator ./cmd/pollinator

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=build /pollinator /pollinator

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/pollinator"]
