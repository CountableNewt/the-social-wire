# Private Social Draft Storage

Web draft text and native settings use Bluesky's private `app.bsky.draft` service.
Social Wire stores attachment files and their manifests in its own private service.
This is an explicitly approved exception to the rebuildable AppView projection
model: these files are canonical user data and cannot be rebuilt from the PDS.
Both services can read their stored data; this is not end-to-end encryption.

## Railway bucket configuration

Use one private media bucket in each environment. The standalone Gateway's
`SOCIAL_DRAFT_S3_` variables for endpoint, bucket, region, access key, and secret
reference the matching `PODCAST_S3_` variables on that service. Draft objects use
opaque 64-character hex keys. Podcast media uses the `clips/` prefix. Drafts
retain their independent `SOCIAL_DRAFT_STORAGE_KEY` for application encryption.
The old `.railway/social-drafts.ts` partial describes the former dedicated
Development bucket and should not be applied for this configuration.

When changing buckets, copy existing encrypted draft objects with their
original keys and verify the copied bytes before switching the references.
Roll out to Development first. Check Gateway readiness and verify that saving
and reopening an article draft preserves its image. Apply the same references
in Production after Development verification.

Configure `SOCIAL_DRAFTS_ENABLED=true` and a 32-byte, standard base64 storage
key. Disabled storage returns unavailable. Incomplete enabled configuration
rejects startup. Draft attachment downloads require viewer authentication.

## Storage and Recovery

Gateway authenticates every request. App View derives the owner from trusted
Gateway identity, encrypts binary files and sensitive metadata, and stores opaque
S3 keys. Downloads authenticate and verify the complete file before serving it
with `Cache-Control: private, no-store`. The Web client also verifies size, MIME
type and SHA-256 before restoring bytes. Draft saving never uploads to public PDS
blobs or posts/schedules a composition.

The limits are 100 MiB per file, 1 GiB of charged files per account and 10,000
charged assets. Original and prepared photos count separately. Reservations and
uploads use immutable client UUIDs; manifests use revision compare-and-swap.
Unbound uploads expire after 24 hours; referenced files have no automatic expiry.
Removing the last reference immediately revokes access and queues physical
deletion. Failed object deletion retries durably and continues charging quota.

Local recovery preserves interrupted work and uncertain native API operations.
The native API has no conditional update or idempotent create, so concurrent edits
require conflict copies rather than a claim of atomic cross-service transactions.
Failed file cleanup retains a completion/deletion marker and an explicit retry;
it must never cause a completed post to be published again.

## Canonical Data Operations

Back up the database manifests/metadata, private bucket contents, and encryption
key as one recoverable data set. A database restore alone does not recover files;
bucket bytes without their key cannot be decrypted. Keep the key in protected
operational secret storage. Do not replace or rotate the single configured key
without a migration that re-encrypts existing data. Never include it in logs,
support exports, source control, or browser configuration.

For an owner-requested export, enumerate that owner's native drafts and retrieve
their manifests and authenticated file downloads; treat the result as private
user content. Account erasure must remove the owner's manifest references and
queue all remaining assets through the durable cleanup path, then confirm physical
deletion before declaring completion. Ordinary projection rebuilds must exclude
all `private_social_draft_*` tables. Do not delete canonical rows ahead of object
cleanup. Draft deletion does not promise immediate erasure from backups or from
Bluesky's private operation history; apply the service's retention obligations to
backups separately.
