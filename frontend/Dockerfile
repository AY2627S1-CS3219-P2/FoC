# AI Assistance Disclosure:
# Tool: Claude Code (model: Opus 5), date: 2026-09-17
# Scope: Added a Node build stage for Vite; the nginx stage is the prototype's,
#   now serving the build output instead of hand-written files.
#   2026-09-22: the build arg is the gateway's URL, not supplier-service's —
#   the browser holds one base URL now (D-010, D-025b).
# Author review: PENDING — <reviewer to complete>

FROM node:22-alpine AS build
WORKDIR /app

# Dependencies are their own layer so a source-only change does not reinstall.
COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig*.json vite.config.ts index.html ./
COPY src ./src

# Vite inlines VITE_* variables at BUILD time, not at container start, so this
# is a build arg rather than a runtime env var. Nothing secret may be passed
# here — it ends up in the bundle that ships to the browser.
#
# The default is http://localhost:8080 because the BROWSER resolves it, and the
# browser runs on the host, where Compose publishes the gateway. It is NOT
# http://api-gateway:8080 — that name only resolves inside the Compose network.
# An empty value would build the fixture app instead (see src/App.tsx).
ARG VITE_GATEWAY_BASE_URL=http://localhost:8080
ENV VITE_GATEWAY_BASE_URL=$VITE_GATEWAY_BASE_URL
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
