import { describe, it, expect } from "bun:test";
import { repoAndPublicationFilterFromPubId } from "@/lib/atprotoClient";

describe("repoAndPublicationFilterFromPubId", () => {
  it("uses plain DID as repo key with no publication filter", () => {
    expect(repoAndPublicationFilterFromPubId("did:plc:abc")).toEqual({
      repoDid: "did:plc:abc",
      publicationAtUri: undefined,
    });
  });

  it("derives repo DID and filter from a publication record AT-URI", () => {
    const uri =
      "at://did:plc:abc/site.standard.publication/3lmn4op56qr7s";
    expect(repoAndPublicationFilterFromPubId(uri)).toEqual({
      repoDid: "did:plc:abc",
      publicationAtUri: uri,
    });
  });

  it("supports com.standard.publication collection", () => {
    const uri = "at://did:plc:xyz/com.standard.publication/rkey1";
    expect(repoAndPublicationFilterFromPubId(uri)).toEqual({
      repoDid: "did:plc:xyz",
      publicationAtUri: uri,
    });
  });
});
