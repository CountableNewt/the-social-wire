FROM swift:6.4.2-jammy AS builder
RUN apt-get update \
  && apt-get install -y --no-install-recommends libssl-dev libcurl4-openssl-dev libsqlite3-dev libxml2-dev libzstd-dev pkg-config \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /src
# Copy canonical package inputs only; local SwiftPM mirrors must not enter images.
COPY services/jetstream-ingest/Package.swift services/jetstream-ingest/Package.resolved /src/services/jetstream-ingest/
COPY services/jetstream-ingest/Sources /src/services/jetstream-ingest/Sources
COPY services/jetstream-ingest/Tests /src/services/jetstream-ingest/Tests
RUN swift build --package-path services/jetstream-ingest -c release --product schema-ready --force-resolved-versions -j 4 \
  && mkdir -p /out/lib /out/bin \
  && cp services/jetstream-ingest/.build/release/schema-ready /out/bin/schema-ready \
  && cp -a /usr/lib/swift/linux/*.so* /out/lib/
COPY database/migrations /src/database/migrations
RUN /out/bin/schema-ready --generate-manifest /src/database/migrations > /out/required-migrations.txt
COPY services/gateway/Package.swift services/gateway/Package.resolved /src/services/gateway/
COPY services/gateway/Sources /src/services/gateway/Sources
COPY services/gateway/Tests /src/services/gateway/Tests
RUN swift build --package-path services/gateway -c release --product Gateway --force-resolved-versions -j 4 \
  && cp services/gateway/.build/release/Gateway /out/bin/Gateway \
  && find -L services/gateway/.build/release/ -maxdepth 1 -type d \( -name '*.resources' -o -name '*.bundle' \) -exec cp -aL {} /out/bin/ \;

FROM ubuntu:22.04
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates libssl3 libcurl4 libsqlite3-0 libicu70 libxml2 libzstd1 \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --system --no-create-home appuser
COPY --from=builder /out/lib/ /usr/lib/swift/linux/
ENV LD_LIBRARY_PATH=/usr/lib/swift/linux
COPY --from=builder /out/bin/ /usr/local/bin/
COPY --from=builder /out/required-migrations.txt /etc/socialwire/required-migrations.txt
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/Gateway"]
