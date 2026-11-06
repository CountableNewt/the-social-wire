"use client";

import { useSidebarProjection } from "@/contexts/PublicationSidebarContext";
import { useUnsubscribePublication } from "@/hooks/usePublications";
import type { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";
import { UnavailableFeedsDialog } from "./UnavailableFeedsDialog";

export function UnavailablePublicationActions({ warning }: { warning: PublicationSourceRefreshError }) {
  const { allPublicationRows } = useSidebarProjection();
  const unsubscribe = useUnsubscribePublication();

  return <UnavailableFeedsDialog warning={warning} publications={allPublicationRows} onUnsubscribe={publication => unsubscribe.mutateAsync({ publication })} />;
}
