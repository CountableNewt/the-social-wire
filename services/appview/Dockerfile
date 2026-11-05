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
COPY services/appview/Package.swift services/appview/Package.resolved /src/services/appview/
COPY services/appview/Sources /src/services/appview/Sources
COPY services/appview/Tests /src/services/appview/Tests
RUN swift build --package-path services/appview -c release --product AppView --force-resolved-versions -j 4 \
  && cp services/appview/.build/release/AppView /out/bin/AppView \
  && find -L services/appview/.build/release/ -maxdepth 1 -type d \( -name '*.resources' -o -name '*.bundle' \) -exec cp -aL {} /out/bin/ \;

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
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/AppView"]
