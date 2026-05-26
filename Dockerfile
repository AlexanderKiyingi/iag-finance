# syntax=docker/dockerfile:1.7
#
# Targets:
#   standalone (default) — iag-finance repo root on Railway
#   monorepo             — IAG_multi_backend root context (deploy/docker-compose)
#
# Monorepo:  docker build -f shared/services/finance/Dockerfile --target monorepo .
# Standalone: docker build -f Dockerfile --target standalone .

FROM golang:1.23-alpine AS base
RUN apk add --no-cache git ca-certificates
ENV PLATFORM_GO_DEP=/deps/platform-go

FROM base AS platform-go-clone
ARG IAG_META_REF=main
ARG IAG_META_REPO=https://github.com/AlexanderKiyingi/IAG_multi_backend.git
RUN git clone --depth 1 --branch "${IAG_META_REF}" "${IAG_META_REPO}" /tmp/iag \
    && mv /tmp/iag/shared/platform-go "${PLATFORM_GO_DEP}" \
    && rm -rf /tmp/iag

FROM base AS platform-go-copy
COPY shared/platform-go ${PLATFORM_GO_DEP}

FROM base AS build-standalone
COPY --from=platform-go-clone ${PLATFORM_GO_DEP} ${PLATFORM_GO_DEP}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /finance ./cmd/server

FROM base AS build-monorepo
COPY --from=platform-go-copy ${PLATFORM_GO_DEP} ${PLATFORM_GO_DEP}
WORKDIR /src/shared/services/finance
COPY shared/services/finance/go.mod shared/services/finance/go.sum ./
RUN go mod edit -replace=github.com/alvor-technologies/iag-platform-go=${PLATFORM_GO_DEP} \
    && go mod download
COPY shared/services/finance/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /finance ./cmd/server

FROM alpine:3.21 AS monorepo
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S iag && adduser -S iag -G iag
WORKDIR /app
COPY --from=build-monorepo /finance .
USER iag
EXPOSE 3006
ENV PORT=3006 ENVIRONMENT=production AUTO_MIGRATE=true
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD sh -c 'wget -qO- "http://127.0.0.1:${PORT}/ready" || exit 1'
CMD ["./finance"]

FROM alpine:3.21 AS standalone
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S iag && adduser -S iag -G iag
WORKDIR /app
COPY --from=build-standalone /finance .
USER iag
EXPOSE 3006
ENV PORT=3006 ENVIRONMENT=production AUTO_MIGRATE=true
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD sh -c 'wget -qO- "http://127.0.0.1:${PORT}/ready" || exit 1'
CMD ["./finance"]
