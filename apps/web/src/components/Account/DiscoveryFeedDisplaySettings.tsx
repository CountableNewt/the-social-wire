"use client";

import { Switch } from "@/components/ui/switch";
import type { FeedDisplayPreferences } from "@/lib/feedPreferences";
import { podcastsEnabled } from "@/lib/podcasts/playback";

const feeds = [
  { id: "social", label: "Social", preference: "showSocial" },
  { id: "podcasts", label: "Podcasts", preference: "showPodcasts" },
] as const;

type DiscoveryFeedDisplaySettingsProps = {
  preferences: Pick<FeedDisplayPreferences, (typeof feeds)[number]["preference"]>;
  isPending: boolean;
  onVisibilityChange: (feed: (typeof feeds)[number]["id"], visible: boolean) => void;
};

export function DiscoveryFeedDisplaySettings({
  preferences,
  isPending,
  onVisibilityChange,
}: DiscoveryFeedDisplaySettingsProps) {
  return feeds.filter(({ id }) => id !== "podcasts" || podcastsEnabled()).map(({ id, label, preference }) => (
    <div
      key={id}
      className="grid min-h-12 grid-cols-[minmax(0,1fr)_5.5rem_5.5rem] items-center py-2 text-sm"
    >
      <span>{label}</span>
      <Switch
        className="justify-self-center"
        checked={preferences[preference]}
        disabled={isPending}
        onCheckedChange={visible => onVisibilityChange(id, visible)}
        aria-label={`Show ${label}`}
      />
      <span
        className="justify-self-center text-muted-foreground"
        aria-label="Unread Count Not Available"
      >
        —
      </span>
    </div>
  ));
}
