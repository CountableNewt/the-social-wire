FROM golang:1.26.6-bookworm AS go-tools
FROM oven/bun:1.3.14 AS builder
RUN apt-get update \
  && apt-get install -y --no-install-recommends git ca-certificates python3 \
  && rm -rf /var/lib/apt/lists/*
COPY --from=go-tools /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY . .
RUN bun install --frozen-lockfile
ARG NEXT_PUBLIC_APP_ENV=dev
ARG NEXT_PUBLIC_SITE_URL=https://testing.thesocialwire.app
ARG NEXT_PUBLIC_SOCIALWIRE_API_URL=https://api.testing.thesocialwire.app
ARG NEXT_PUBLIC_USE_THIN_APPVIEW=true
ARG NEXT_PUBLIC_PDS_READ_STATE_ENABLED=true
ARG NEXT_PUBLIC_PDS_READ_STATE_GC_ENABLED=false
ARG NEXT_PUBLIC_PODCASTS_ENABLED=true
ARG NEXT_PUBLIC_TOOLS_ENABLED
ARG NEXT_PUBLIC_PRO_GATING_ENABLED=false
ARG NEXT_PUBLIC_ENABLE_POST_SCHEDULING=false
ARG NEXT_PUBLIC_FINANCE_TICKER_ENABLED=false
ARG NEXT_PUBLIC_FINANCE_WIDGETS_ENABLED=false
ARG NEXT_PUBLIC_WIRE_NEWS_EDITION_ENABLED=true
ARG NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED=false
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
