FROM node:22-alpine AS css

WORKDIR /app

COPY package.json ./
RUN npm install

COPY assets ./assets
COPY web ./web

RUN npm run build:css

FROM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod ./
COPY . .

COPY --from=css /app/web/static/app.css ./web/static/app.css

# go.sum isn't committed to the repo (see README), so it's generated here,
# with the full source present, rather than relying on an early
# go-mod-only layer whose cache can go stale without anything else in the
# Dockerfile changing to invalidate it.
RUN go mod tidy

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /pollinator ./cmd/pollinator

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /

COPY --from=build /pollinator /pollinator

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/pollinator"]
