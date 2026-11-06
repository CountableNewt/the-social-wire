import { expect, it } from "bun:test";
import { PublicationSourceRefreshError, sourceRefreshErrorFromResponse } from "@/lib/publicationSourceRefreshError";

const failure = { category: "rss_backoff", kind: "other", host: "publisher.test", count: 1 };
const envelope = { error: "Service Unavailable", message: "Enrollment unavailable", articlesUpdated: true, sourceFailures: [failure] };

it("exposes only sanitized publisher hosts for a completed RSS-only update", () => {
  const error = sourceRefreshErrorFromResponse(503, { ...envelope, sourceFailures: [failure, failure, { ...failure, host: "another.test" }] });
  expect(error).toBeInstanceOf(PublicationSourceRefreshError);
  expect(error?.hosts).toEqual(["another.test", "publisher.test"]);
  expect(error?.statusMessage).toBe("Articles Updated — Some Sources Unavailable");
  expect(error?.message).toContain("publisher.test");
});

it("never labels auth, infrastructure, cancellation, or malformed failures as updated articles", () => {
  for (const status of [200, 400, 401, 403, 500]) {
    expect(sourceRefreshErrorFromResponse(status, envelope)).toBeUndefined();
  }
  for (const body of [null, {}, { articlesUpdated: true, sourceFailures: [failure] }, { ...envelope, error: "Unauthorized" }, { ...envelope, message: null }, { ...envelope, articlesUpdated: false }, { ...envelope, sourceFailures: [] },
    ...[{ category: "rss_store" }, { category: "pds_fetch" }, { kind: "canceled" }, { kind: "database" }, { host: "https://user:secret@host.test/private?token=secret" }, { count: -1 }, { count: "1" }, { status: 0 }, { status: "503" }, { status: 600 }].map(change => ({ ...envelope, sourceFailures: [{ ...failure, ...change }] }))]) {
    expect(sourceRefreshErrorFromResponse(503, body)).toBeUndefined();
  }
});

it("retains exact source identities without matching feeds solely by their shared host", () => {
  const ids = ["rss:world", "rss:technology", "rss:world"];
  expect(sourceRefreshErrorFromResponse(503, { ...envelope, unavailablePublicationIds: ids })?.unavailablePublicationIds).toEqual(["rss:technology", "rss:world"]);
  expect(sourceRefreshErrorFromResponse(503, envelope)?.unavailablePublicationIds).toEqual([]);
  for (const unavailablePublicationIds of ["rss:world", [null], ["at://did:plc:viewer/site.standard.publication/main"], ["rss:"]]) {
    expect(sourceRefreshErrorFromResponse(503, { ...envelope, unavailablePublicationIds })).toBeUndefined();
  }
});
