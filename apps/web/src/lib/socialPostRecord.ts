import { AppBskyFeedPost } from "@atproto/api";

/** Read complete upstream posts without applying our composer's text limits. */
export function validateSocialPostRecord(value: unknown): ReturnType<typeof AppBskyFeedPost.validateRecord> {
  if (!value || typeof value !== "object" || !("text" in value) || typeof value.text !== "string") return AppBskyFeedPost.validateRecord(value);
  // Validate every other field with the SDK's unchanged record schema. Keep
  // the original string and UTF-8 facet offsets for rendering and moderation.
  const result = AppBskyFeedPost.validateRecord({ ...value, text: "" });
  return result.success ? { ...result, value: { ...result.value, text: value.text } } : result;
}
