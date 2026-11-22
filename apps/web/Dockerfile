FROM oven/bun:1.3.14 AS builder
RUN apt-get update \
  && apt-get install -y --no-install-recommends git ca-certificates python3 procps \
  && rm -rf /var/lib/apt/lists/*
ENV NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/package.json
COPY packages/read-state/package.json packages/read-state/package.json
COPY packages/lexicons/package.json packages/lexicons/package.json
COPY packages/spec/package.json packages/spec/package.json
COPY packages/vendor packages/vendor
RUN bun install --frozen-lockfile
COPY . .
ARG NEXT_PUBLIC_APP_ENV=dev
ARG NEXT_PUBLIC_SITE_URL=https://testing.thesocialwire.app
ARG NEXT_PUBLIC_SOCIALWIRE_API_URL=https://api.testing.thesocialwire.app
ARG NEXT_PUBLIC_LIVESTREAM_ENABLED=true
ARG NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED=false
ARG NEXT_PUBLIC_ENABLE_POST_SCHEDULING=false
ENV NEXT_STANDALONE=true
RUN bun run turbo run build --filter=web... --env-mode=loose

FROM node:24.19.0-bookworm-slim AS runtime
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
WORKDIR /app
COPY --from=builder --chown=node:node /app/apps/web/.next/standalone ./
COPY --from=builder --chown=node:node /app/apps/web/public ./apps/web/public
COPY --from=builder --chown=node:node /app/apps/web/.next/static ./apps/web/.next/static
USER node
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
