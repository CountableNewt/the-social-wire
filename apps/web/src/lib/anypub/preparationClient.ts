import type { ArticleRecord } from "@/lib/articles/articlePublishingTypes";

/** The preparation endpoint handles document data without account credentials. */
export async function requestPreparation<T>(body: ArticleRecord, key: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch("/xrpc/at.anypub.prepare", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    credentials: "omit",
    redirect: "error",
    cache: "no-store",
    signal: signal ?? AbortSignal.timeout(30_000),
  });
  const result = await response.json() as Record<string, unknown> | null;
  if (!response.ok) {
    throw new Error(typeof result?.message === "string" ? result.message : `Publishing Preparation Failed (${response.status}).`);
  }
  const value = result?.[key];
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("The Publishing Service Returned an Invalid Preparation Result.");
  }
  return value as T;
}
