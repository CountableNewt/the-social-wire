# Client Cache Versus AppView and Index Staleness

1. Capture the frontend request ID and `traceparent`.
2. Check response freshness and AppView index age for the exact route template.
3. If AppView is fresh, invalidate or inspect the client React Query/IndexedDB cache.
4. If AppView is stale, inspect worker committed age, gaps, and projection-cache state.
5. Do not repair a server completeness issue with reader/sidebar UI changes.
