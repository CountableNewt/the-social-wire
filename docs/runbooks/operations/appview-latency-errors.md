# AppView Latency and Error Investigation

1. Filter traces by critical route and status class.
2. Separate Gateway proxy time from AppView auth, cache, query, and bootstrap phase spans.
3. Compare route p95 against bootstrap 5s, entries 2s, unread 1.5s, and sidebar 3s thresholds.
4. Inspect only bounded query names and cache outcomes; never add DIDs, URLs, bodies, or error strings as dimensions.
5. Correlate response index age with ingestion freshness before changing query or cache behavior.
