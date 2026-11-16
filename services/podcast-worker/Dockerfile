FROM swift:6.4.2-jammy AS schema-ready-builder
WORKDIR /src
# Copy canonical package inputs only; local SwiftPM mirrors must not enter images.
COPY tools/schema-ready/Package.swift tools/schema-ready/Package.resolved /src/tools/schema-ready/
COPY tools/schema-ready/Sources /src/tools/schema-ready/Sources
COPY tools/schema-ready/Tests /src/tools/schema-ready/Tests
# The Bun runtime image has no Swift libraries, so link the Swift runtime statically.
RUN swift build --package-path tools/schema-ready -c release --product schema-ready --static-swift-stdlib --force-resolved-versions -j 4 \
  && mkdir -p /out \
  && cp tools/schema-ready/.build/release/schema-ready /out/schema-ready
COPY database/migrations /src/database/migrations
RUN /out/schema-ready --generate-manifest /src/database/migrations > /out/required-migrations.txt

FROM oven/bun:1.3.14-debian
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg fonts-dejavu-core ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/package.json
COPY apps/operations/package.json apps/operations/package.json
COPY packages/read-state/package.json packages/read-state/package.json
COPY packages/lexicons/package.json packages/lexicons/package.json
COPY packages/spec/package.json packages/spec/package.json
COPY services/podcast-worker/package.json services/podcast-worker/package.json
COPY packages/vendor packages/vendor
RUN bun install --frozen-lockfile --ignore-scripts
COPY services/podcast-worker services/podcast-worker
USER bun
EXPOSE 8080
COPY --from=schema-ready-builder /out/schema-ready /usr/local/bin/schema-ready
COPY --from=schema-ready-builder /out/required-migrations.txt /etc/socialwire/required-migrations.txt
ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/bun", "services/podcast-worker/src/index.ts"]
