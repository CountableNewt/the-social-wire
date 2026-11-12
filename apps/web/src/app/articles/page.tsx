import { Suspense } from "react";
import { ArticlesWorkspace } from "@/components/Articles/ArticlesWorkspace";

export default async function ArticlesPage({ searchParams }: { searchParams: Promise<{ draft?: string | string[] }> }) {
  const { draft } = await searchParams;
  return <Suspense fallback={<p role="status">Loading Articles…</p>}><ArticlesWorkspace initialDraftId={typeof draft === "string" ? draft : undefined} /></Suspense>;
}
