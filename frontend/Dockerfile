# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5), date: 2026-09-17
# Scope: Added a Node build stage for Vite; the nginx stage is the prototype's,
#   now serving the build output instead of hand-written files.
# Author review: PENDING — <reviewer to complete>

FROM node:22-alpine AS build
WORKDIR /app

# Dependencies are their own layer so a source-only change does not reinstall.
COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig*.json vite.config.ts index.html ./
COPY src ./src

# Vite inlines VITE_* variables at BUILD time, not at container start, so this
# is a build arg rather than a runtime env var. The default suits the Compose
# demo: the browser runs on the host, where supplier-service is published on
# 8082. Nothing secret may be passed here — it ends up in the bundle.
ARG VITE_SUPPLIER_BASE_URL=http://localhost:8082
ENV VITE_SUPPLIER_BASE_URL=$VITE_SUPPLIER_BASE_URL
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
