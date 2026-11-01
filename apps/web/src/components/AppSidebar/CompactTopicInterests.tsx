"use client";

import { SidebarTopicInterestLinks } from "./SidebarTopicInterestLinks";
import { useTopicSidebarInterests } from "@/hooks/useTopicSidebarInterests";

export function CompactTopicInterests({ topic, onNavigate }: {
  topic: "finance" | "sports";
  onNavigate?: (href: string) => void;
}) {
  const interests = useTopicSidebarInterests(topic);
  return <SidebarTopicInterestLinks topic={topic} {...interests} onNavigate={onNavigate} />;
}
