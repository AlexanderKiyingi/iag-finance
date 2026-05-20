FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /finance ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && addgroup -S iag && adduser -S iag -G iag
WORKDIR /app
COPY --from=build /finance .
USER iag
EXPOSE 3006
ENV PORT=3006
ENV ENVIRONMENT=production
ENV AUTO_MIGRATE=true
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD sh -c 'wget -qO- "http://127.0.0.1:${PORT}/ready" || exit 1'
CMD ["./finance"]
