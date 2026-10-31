# syntax=docker/dockerfile:1
# Build from the repository root: the guide imports docs/integration.md outside web/.
FROM node:22-alpine AS build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY docs/ /src/docs/
RUN npm run build

FROM nginx:alpine
# Keep port 80, but run the master and worker without root privileges.
# Compose permits unprivileged binding and bounds resources on shared hosts.
RUN sed -i '/^user[[:space:]]/d; s@^[[:space:]]*pid[[:space:]][^;]*;@pid /tmp/nginx.pid;@; s/worker_processes[[:space:]]*auto;/worker_processes 1;/' /etc/nginx/nginx.conf \
    && grep -q '^pid /tmp/nginx.pid;' /etc/nginx/nginx.conf \
    && chown -R nginx:nginx /var/cache/nginx
COPY --from=build --chown=nginx:nginx /src/web/dist /usr/share/nginx/html
COPY web/nginx.conf /etc/nginx/conf.d/default.conf
USER nginx
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1/api/status || exit 1
CMD ["nginx", "-g", "daemon off;"]
