import { SocialPermissionRecovery } from "@/components/Social/SocialPermissionRecovery";
import { Button } from "@/components/ui/button";
import { metricLabel, safeAnalyticsUrl } from "@/lib/analytics/creatorAnalyticsClient";
import type { PublicationAnalyticsSummary } from "@/lib/analytics/publicationAnalyticsClient";

function publicationCount(value: number | null, complete: boolean): string {
  if (value == null) return "Unavailable";
  return complete ? metricLabel(value) : `At least ${metricLabel(value)}`;
}

export function PublicationAnalytics({ data, isPending, error, retry }: {
  data?: PublicationAnalyticsSummary;
  isPending: boolean;
  error: unknown;
  retry: () => void;
}) {
  const message = error instanceof Error ? error.message : error ? "Publication analytics could not be loaded. Please try again." : undefined;
  return <section aria-label="Publication Analytics" className="space-y-3 border-t pt-4">
    <div>
      <h2 className="font-semibold">Publication Analytics</h2>
      <p className="mt-1 text-xs text-muted-foreground">
        Current Public Counts{data ? ` · Updated ${new Date(data.sampledAt).toLocaleString()}` : ""}
      </p>
    </div>
    {isPending ? <p role="status" className="text-sm text-muted-foreground">Loading Publication Analytics…</p> : null}
    {message ? <div role="alert" className="rounded-xl border border-destructive/40 p-4 text-sm">
      <p>{message}</p>
      {/log in again/i.test(message) ? <SocialPermissionRecovery /> : <Button size="sm" variant="outline" className="mt-2" onClick={retry}>Retry Publication Analytics</Button>}
    </div> : null}
    {data && !data.inventoryComplete ? <p className="text-sm text-muted-foreground">
      The publication inventory is incomplete. Some publications or documents may be missing.
    </p> : null}
    {data?.publications.length === 0 ? <p className="text-sm text-muted-foreground">
      {data.inventoryComplete ? "No standard.site publications found for this account." : "No publications were found in the available records."}
    </p> : null}
    {data?.publications.length ? <ul className="grid gap-3 lg:grid-cols-2">
      {data.publications.map(publication => {
        const href = publication.url ? safeAnalyticsUrl(publication.url) : undefined;
        const name = publication.name || "Untitled Publication";
        return <li key={publication.uri} className="min-w-0 space-y-3 rounded-2xl border bg-card p-4">
          <h3 className="break-words text-sm font-medium">
            {href ? <a href={href} className="hover:underline">{name}</a> : name}
          </h3>
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <div>
              <dt className="text-xs text-muted-foreground">Documents</dt>
              <dd className="mt-1 font-semibold tabular-nums">{publicationCount(publication.documents, data.inventoryComplete)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Subscribers</dt>
              <dd className="mt-1 font-semibold tabular-nums">{publicationCount(publication.subscribers, true)}</dd>
            </div>
            <div>
              <dt className="break-words text-xs text-muted-foreground">Recommendations</dt>
              <dd className="mt-1 font-semibold tabular-nums">{publicationCount(publication.recommendations, publication.recommendationCoverageComplete)}</dd>
            </div>
          </dl>
        </li>;
      })}
    </ul> : null}
  </section>;
}
