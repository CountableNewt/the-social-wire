# Go AppView API Test Plan

The public backend lives in `services/appview`. Tests colocate with Go packages and cover OAuth/DPoP admission, reader bootstrap, read-state boundaries, drafts, scheduling and SQLite migrations.

```sh
(cd services/appview && go test ./... && go vet ./...)
(cd packages/spec && bun test)
```

API contract changes must update `packages/spec/openapi.yaml`, the endpoint manifest and matching Bruno requests under `services/appview/bruno`. Route drift inspects Go registration sources. Native and Web clients share the same API hosts and authenticated NDJSON bootstrap contract.

Before release, validate signed-in reader and authoring workflows in Development, including nonce retries, account changes, pagination, unread reconciliation and scheduling. Promote the same source to Production only after Development acceptance.
