import { describe, it, expect } from "bun:test";
import {
  normalizeAtRepoParam,
  publicationRepoDid,
  repoAndPublicationFilterFromPubId,
  viewerOwnsDiscoveredPublication,
} from "@/lib/atprotoClient";

describe("normalizeAtRepoParam", () => {
  it("decodes a single-encoded DID segment to a DID", () => {
    expect(normalizeAtRepoParam("did%3Aplc%3Axyz")).toBe("did:plc:xyz");
  });

  it("decodes a double-encoded DID segment", () => {
    expect(normalizeAtRepoParam("did%253Aplc%253Axyz")).toBe("did:plc:xyz");
  });

  it("trims and strips a leading @ before decoding", () => {
    expect(normalizeAtRepoParam("  @did%3Aplc%3Ax  ")).toBe("did:plc:x");
  });
});

describe("repoAndPublicationFilterFromPubId", () => {
  it("uses plain DID as repo key with no publication filter", () => {
    expect(repoAndPublicationFilterFromPubId("did:plc:abc")).toEqual({
      repoDid: "did:plc:abc",
      publicationAtUri: undefined,
    });
  });

  it("accepts a URL-encoded DID as repo key", () => {
    expect(repoAndPublicationFilterFromPubId("did%3Aplc%3Aabc")).toEqual({
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

  it("falls back to repoDid as full AT-URI for non-publication collections", () => {
    const uri = "at://did:plc:self/site.standard.document/key1";
    expect(repoAndPublicationFilterFromPubId(uri)).toEqual({
      repoDid: uri,
      publicationAtUri: undefined,
    });
  });
});

describe("publicationRepoDid", () => {
  it("returns the DID from a plain repo key", () => {
    expect(publicationRepoDid("did:plc:abc")).toBe("did:plc:abc");
  });

  it("returns repo owner from a publication AT-URI", () => {
    const uri =
      "at://did:plc:mine/site.standard.publication/3lmn4op56qr7s";
    expect(publicationRepoDid(uri)).toBe("did:plc:mine");
  });

  it("extracts DID when repoAndPublicationFilterFromPubId returns an entry-style AT-URI", () => {
    const uri = "at://did:plc:self/site.standard.entry/rkey77";
    expect(publicationRepoDid(uri)).toBe("did:plc:self");
  });
});

describe("viewerOwnsDiscoveredPublication", () => {
  const me = "did:plc:viewer123";

  it("is true when publicationId is the viewer DID (aggregate feed)", () => {
    expect(
      viewerOwnsDiscoveredPublication({ publicationId: me }, me)
    ).toBe(true);
  });

  it("is true when publicationId is an AT-URI on the viewer repo", () => {
    expect(
      viewerOwnsDiscoveredPublication(
        {
          publicationId:
            "at://did:plc:viewer123/site.standard.publication/key1",
        },
        me
      )
    ).toBe(true);
  });

  it("is false for another repo", () => {
    expect(
      viewerOwnsDiscoveredPublication(
        { publicationId: "did:plc:someoneelse" },
        me
      )
    ).toBe(false);
  });

  it("does not depend on authorDid (repo id is source of truth)", () => {
    expect(
      viewerOwnsDiscoveredPublication(
        {
          publicationId:
            "at://did:plc:viewer123/com.standard.publication/r1",
        },
        me
      )
    ).toBe(true);
  });
});
