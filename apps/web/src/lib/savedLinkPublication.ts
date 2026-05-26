import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { MergedLatrSave } from "@/lib/pdsClient";
import type { SidebarPublicationRow } from "@/lib/publicationProjectionClient";
import { sidebarRowToDiscoveredPublication } from "@/lib/publicationProjectionClient";
import { resolveSavedLinkEmbedUrl } from "@/lib/savedLinkEmbedUrl";

export type SavedLinkPublication = {
  name: string;
  faviconUrl?: string;
  homepageUrl?: string;
};

export function siteHostKey(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  try {
    const u = trimmed.includes("://")
      ? new URL(trimmed)
      : new URL(`https://${trimmed}`);
    return u.hostname.replace(/^www\./i, "").toLowerCase();
  } catch {
    return null;
  }
}

function originFromUrl(raw: string): string | undefined {
  try {
    return new URL(raw).origin;
  } catch {
    return undefined;
  }
}

function faviconUrlForOrigin(origin: string): string {
  return `${origin.replace(/\/$/, "")}/favicon.ico`;
}

function isBareHostname(value: string): boolean {
  const t = value.trim();
  return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(t) && !t.includes(" ");
}

function savedLinkSiteDisplayName(site: string): string {
  const trimmed = site.trim();
  if (/^https?:\/\//i.test(trimmed)) {
    return siteHostKey(trimmed) ?? trimmed;
  }
  if (isBareHostname(trimmed)) {
    return trimmed.replace(/^www\./i, "");
  }
  return trimmed;
}

function parseAtUriAuthorDid(atUri: string): string | null {
  const m = /^at:\/\/([^/]+)\//.exec(atUri.trim());
  return m?.[1] ?? null;
}

/** Hostnames derived from saved-link URLs and site metadata. */
export function articleHostKeysForSavedLink(row: MergedLatrSave): Set<string> {
  const keys = new Set<string>();
  const candidates = [
    resolveSavedLinkEmbedUrl(row),
    row.linkedWebUrl,
    row.kind === "external" ? row.url : row.url,
    row.site,
  ];
  for (const candidate of candidates) {
    if (!candidate?.trim()) continue;
    const key = siteHostKey(candidate);
    if (key) keys.add(key);
  }
  return keys;
}

export function matchSavedLinkPublicationFromSidebar(
  row: MergedLatrSave,
  sidebarRows: SidebarPublicationRow[]
): DiscoveredPublication | null {
  const articleHosts = articleHostKeysForSavedLink(row);

  if (articleHosts.size > 0) {
    for (const sidebarRow of sidebarRows) {
      for (const siteUrl of sidebarRow.appViewScope.publicationSiteUrls) {
        const host = siteHostKey(siteUrl);
        if (host && articleHosts.has(host)) {
          return sidebarRowToDiscoveredPublication(sidebarRow);
        }
      }
    }
  }

  if (row.kind === "native") {
    const authorDid = parseAtUriAuthorDid(row.subjectUri);
    if (authorDid) {
      const matches = sidebarRows.filter((r) => r.authorDid === authorDid);
      if (matches.length === 1) {
        return sidebarRowToDiscoveredPublication(matches[0]);
      }
    }
  }

  return null;
}

/** Resolve publication label + favicon for a merged read-later row. */
export function resolveSavedLinkPublication(
  row: MergedLatrSave
): SavedLinkPublication | null {
  const articleUrl =
    resolveSavedLinkEmbedUrl(row) ??
    row.linkedWebUrl?.trim() ??
    (row.kind === "external" ? row.url.trim() : row.url?.trim());

  const homepageUrl = articleUrl ? originFromUrl(articleUrl) : undefined;

  const nameFromSite = row.site?.trim()
    ? savedLinkSiteDisplayName(row.site)
    : undefined;
  const nameFromUrl =
    articleUrl && siteHostKey(articleUrl)
      ? siteHostKey(articleUrl)!.replace(/^www\./i, "")
      : undefined;
  const name = nameFromSite ?? nameFromUrl;
  if (!name) return null;

  const faviconFromSite =
    row.site?.trim() && /^https?:\/\//i.test(row.site.trim())
      ? faviconUrlForOrigin(originFromUrl(row.site.trim()) ?? row.site.trim())
      : undefined;
  const faviconUrl =
    (articleUrl && homepageUrl ? faviconUrlForOrigin(homepageUrl) : undefined) ??
    faviconFromSite;

  return {
    name,
    ...(faviconUrl ? { faviconUrl } : {}),
    ...(homepageUrl ? { homepageUrl } : {}),
  };
}

export function enrichSavedLinkPublication(
  base: SavedLinkPublication,
  sidebarMatch: DiscoveredPublication | null
): SavedLinkPublication {
  if (!sidebarMatch) return base;
  return {
    name: sidebarMatch.title?.trim() || base.name,
    faviconUrl: sidebarMatch.iconUrl?.trim() || base.faviconUrl,
    homepageUrl: base.homepageUrl,
  };
}

export function resolveSavedLinkPublicationWithSidebar(
  row: MergedLatrSave,
  sidebarRows: SidebarPublicationRow[]
): SavedLinkPublication | null {
  const sidebarMatch = matchSavedLinkPublicationFromSidebar(row, sidebarRows);
  const base = resolveSavedLinkPublication(row);

  if (base) {
    return enrichSavedLinkPublication(base, sidebarMatch);
  }

  if (!sidebarMatch?.title?.trim()) return null;

  return {
    name: sidebarMatch.title.trim(),
    ...(sidebarMatch.iconUrl?.trim()
      ? { faviconUrl: sidebarMatch.iconUrl.trim() }
      : {}),
  };
}
