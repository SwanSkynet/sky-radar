# syntax=docker/dockerfile:1
# Builds the static frontend and serves it from nginx. Build context is
# the repo root (see deploy/fly/web.toml). VITE_API_BASE_URL is inlined by
# Vite at build time, so it must be passed as a build arg, not a runtime env var.
FROM node:22-alpine AS build
ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
WORKDIR /app
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build

FROM nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
RUN touch /var/run/nginx.pid && \
    chown -R nginx:nginx /var/run/nginx.pid /var/cache/nginx /usr/share/nginx/html
USER nginx
EXPOSE 8080
