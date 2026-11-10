import { AppBskyRichtextFacet, RichText } from "@atproto/api";
import { isValidDid, isValidHandle } from "@atproto/syntax";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { socialHttpsUrl } from "@/components/Social/socialUrls";

/** SDK byte offsets are preserved; unresolved mention handles remain plain text. */
export function detectProfileBioFacets(text: string): AppBskyRichtextFacet.Main[] {
  const richText = new RichText({ text });
  richText.detectFacetsWithoutResolution();
  return (richText.facets ?? []).flatMap(facet => {
    const features = facet.features.filter(feature => {
      if (AppBskyRichtextFacet.isLink(feature)) return !!socialHttpsUrl(feature.uri);
      if (AppBskyRichtextFacet.isMention(feature)) return isValidHandle(feature.did);
      return AppBskyRichtextFacet.isTag(feature);
    });
    return features.length ? [{ ...facet, features }] : [];
  });
}

/** Public identity lookup only: no OAuth token, profile write, or stored facet. */
export async function resolveProfileBioFacets(text: string, signal?: AbortSignal): Promise<AppBskyRichtextFacet.Main[]> {
  signal?.throwIfAborted();
  const facets = detectProfileBioFacets(text);
  const handles = new Set(facets.flatMap(facet => facet.features.flatMap(feature => AppBskyRichtextFacet.isMention(feature) ? [feature.did.toLowerCase()] : [])));
  if (!handles.size) return facets;
  const endpoint = getAtprotoNetwork().publicAppView;
  const resolved = new Map<string, string>();
  await Promise.all([...handles].map(async handle => {
    const url = new URL("/xrpc/com.atproto.identity.resolveHandle", endpoint);
    url.search = new URLSearchParams({ handle }).toString();
    try {
      signal?.throwIfAborted();
      const response = await fetch(url.href, { credentials: "omit", redirect: "error", signal, headers: { Accept: "application/json" } });
      signal?.throwIfAborted();
      if (!response.ok) return;
      const data: unknown = await response.json();
      signal?.throwIfAborted();
      if (data && typeof data === "object" && "did" in data && isValidDid(data.did)) resolved.set(handle, data.did);
    } catch (error) {
      signal?.throwIfAborted();
      if (error instanceof Error && error.name === "AbortError") throw error;
      // Unresolved mentions remain readable while links and tags still work.
    }
  }));
  signal?.throwIfAborted();
  return facets.flatMap(facet => {
    const features = facet.features.flatMap(feature => {
      if (!AppBskyRichtextFacet.isMention(feature)) return [feature];
      const did = resolved.get(feature.did.toLowerCase());
      return did ? [{ ...feature, did }] : [];
    });
    return features.length ? [{ ...facet, features }] : [];
  });
}
