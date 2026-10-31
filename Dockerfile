# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY docs/ /src/docs/
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src/backend
COPY backend/ ./
RUN go mod download && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
COPY --from=backend --chown=app:app /out/server /app/server
COPY --from=web --chown=app:app /src/web/dist /app/web/dist
ENV PORT=8080 STATIC_DIR=/app/web/dist STATISTICS_TIMEZONE=Asia/Shanghai
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
