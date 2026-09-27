# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5), date: 2026-09-17
# Scope: Added a Node build stage for Vite; the nginx stage is the prototype's,
#   now serving the build output instead of hand-written files.
#   2026-09-22: the build arg is the gateway's URL, not supplier-service's —
#   the browser holds one base URL now (D-010, D-025b).
# Author review: Nigeltzy - See docker file updates in other branch, this is deprecated, originally used to work in absence of other services, created boilerplate.

FROM node:22-alpine AS build
WORKDIR /app

# Dependencies are their own layer so a source-only change does not reinstall.
COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig*.json vite.config.ts index.html ./
COPY src ./src

# VITE_GATEWAY_BASE_URL is inlined into the bundle at build time, so it is a
# build arg, not a runtime env var. Nothing secret may be passed here: it ships
# to the browser.
#
# Read by src/lib/config.ts. Empty makes the app call the API with relative
# paths on the page's own origin. The default below is a different origin from
# the page this image serves.
ARG VITE_GATEWAY_BASE_URL=http://localhost:8080
ENV VITE_GATEWAY_BASE_URL=$VITE_GATEWAY_BASE_URL
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
