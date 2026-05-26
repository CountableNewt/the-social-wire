import { describe, expect, it } from "bun:test";

import type { MergedLatrSave } from "@/lib/pdsClient";
import type { SidebarPublicationRow } from "@/lib/publicationProjectionClient";
import {
  articleHostKeysForSavedLink,
  enrichSavedLinkPublication,
  matchSavedLinkPublicationFromSidebar,
  resolveSavedLinkPublication,
  resolveSavedLinkPublicationWithSidebar,
  siteHostKey,
} from "@/lib/savedLinkPublication";

const externalRow: MergedLatrSave = {
  kind: "external",
  normalizedUrl: "https://adventures.samclemente.me/a/post",
  url: "https://adventures.samclemente.me/a/post",
  savedAt: "2026-01-01T00:00:00.000Z",
  externalRkey: "ext",
  itemRkey: "item",
  externalUri: "at://did/com.latr.saved.external/ext",
  itemUri: "at://did/com.latr.saved.item/item",
  subjectUri: "at://did/com.latr.saved.external/ext",
  site: "standard.site",
};

describe("savedLinkPublication", () => {
  it("normalizes site host keys", () => {
    expect(siteHostKey("https://www.example.com/path")).toBe("example.com");
    expect(siteHostKey("example.com")).toBe("example.com");
  });

  it("resolves publication name from external site metadata", () => {
    const pub = resolveSavedLinkPublication(externalRow);
    expect(pub?.name).toBe("standard.site");
    expect(pub?.faviconUrl).toBe(
      "https://adventures.samclemente.me/favicon.ico"
    );
  });

  it("prefers display site names over bare hostnames", () => {
    const row: MergedLatrSave = {
      ...externalRow,
      site: "Sam's Adventures",
    };
    expect(resolveSavedLinkPublication(row)?.name).toBe("Sam's Adventures");
  });

  it("collects article host keys from external URLs", () => {
    const keys = articleHostKeysForSavedLink(externalRow);
    expect(keys.has("adventures.samclemente.me")).toBe(true);
    expect(keys.has("standard.site")).toBe(true);
  });

  it("matches sidebar publications by site URL hostname", () => {
    const sidebarRows: SidebarPublicationRow[] = [
      {
        publicationId: "at://did/site.standard.publication/abc",
        authorDid: "did:plc:author",
        authorHandle: "sam",
        title: "Sam's Adventures",
        iconUrl: "https://cdn.example/icon.png",
        discoveredAt: "2026-01-01T00:00:00.000Z",
        appViewScope: {
          authorDid: "did:plc:author",
          publicationAtUri: "at://did/site.standard.publication/abc",
          publicationScopeAtUris: [],
          publicationSiteUrls: ["https://adventures.samclemente.me"],
        },
      },
    ];

    const match = matchSavedLinkPublicationFromSidebar(externalRow, sidebarRows);
    expect(match?.title).toBe("Sam's Adventures");
  });

  it("enriches base publication metadata with sidebar title and icon", () => {
    const enriched = enrichSavedLinkPublication(
      { name: "standard.site", faviconUrl: "https://adventures.samclemente.me/favicon.ico" },
      {
        publicationId: "at://did/site.standard.publication/abc",
        authorDid: "did:plc:author",
        authorHandle: "sam",
        title: "Sam's Adventures",
        iconUrl: "https://cdn.example/icon.png",
        discoveredAt: "2026-01-01T00:00:00.000Z",
      }
    );
    expect(enriched.name).toBe("Sam's Adventures");
    expect(enriched.faviconUrl).toBe("https://cdn.example/icon.png");
  });

  it("resolves native rows from preview site metadata", () => {
    const nativeRow: MergedLatrSave = {
      kind: "native",
      savedAt: "2026-01-01T00:00:00.000Z",
      itemRkey: "item",
      itemUri: "at://did/com.latr.saved.item/item",
      subjectUri: "at://did:plc:author/site.standard.document/post",
      site: "Build Notes",
      url: "https://notes.example.com/post",
    };
    expect(resolveSavedLinkPublication(nativeRow)?.name).toBe("Build Notes");
  });

  it("matches native rows to a single sidebar publication by author DID", () => {
    const nativeRow: MergedLatrSave = {
      kind: "native",
      savedAt: "2026-01-01T00:00:00.000Z",
      itemRkey: "item",
      itemUri: "at://did/com.latr.saved.item/item",
      subjectUri: "at://did:plc:author/site.standard.document/post",
    };
    const sidebarRows: SidebarPublicationRow[] = [
      {
        publicationId: "at://did/site.standard.publication/abc",
        authorDid: "did:plc:author",
        authorHandle: "sam",
        title: "Build Notes",
        iconUrl: "https://cdn.example/build.png",
        discoveredAt: "2026-01-01T00:00:00.000Z",
        appViewScope: {
          authorDid: "did:plc:author",
          publicationAtUri: "at://did/site.standard.publication/abc",
          publicationScopeAtUris: [],
          publicationSiteUrls: [],
        },
      },
    ];
    const pub = resolveSavedLinkPublicationWithSidebar(nativeRow, sidebarRows);
    expect(pub?.name).toBe("Build Notes");
    expect(pub?.faviconUrl).toBe("https://cdn.example/build.png");
  });
});
