import { RichText } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { createOAuthAgent } from "@/lib/atprotoClient";
import { isMissingOAuthScope, SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";

const PROFILE_COLLECTION = "app.bsky.actor.profile";
export const PROFILE_IMAGE_MAX_BYTES = 1_000_000;

export type EditableProfileSnapshot = {
  record: Record<string, unknown>;
  cid: string | null;
};

export type SaveEditableProfileOptions = {
  session: OAuthSession;
  expectedDid: string;
  snapshot: EditableProfileSnapshot;
  displayName: string;
  description: string;
  /** Undefined preserves the existing image, null removes it. */
  avatar?: File | null;
  banner?: File | null;
  assertCurrent?: () => void;
  signal?: AbortSignal;
};

function errorCode(error: unknown): string | undefined {
  if (error && typeof error === "object" && "error" in error && typeof error.error === "string") return error.error;
}

export function validateProfileImage(file: File): void {
  if (!["image/jpeg", "image/png"].includes(file.type)) throw new Error("Profile images must be JPEG or PNG files.");
  if (!file.size || file.size > PROFILE_IMAGE_MAX_BYTES) throw new Error("Profile images must be between 1 byte and 1 MB.");
}

export function validateProfileText(displayName: string, description: string): void {
  const length = (text: string) => ({ characters: new RichText({ text }).graphemeLength, bytes: new TextEncoder().encode(text).length });
  const name = length(displayName), bio = length(description);
  if (name.characters > 64 || name.bytes > 640) throw new Error("Display name must be at most 64 characters and 640 UTF-8 bytes.");
  if (bio.characters > 256 || bio.bytes > 2560) throw new Error("Bio must be at most 256 characters and 2,560 UTF-8 bytes.");
}

/** Public blob URL from the session's PDS audience, never the OAuth issuer. */
export async function editableProfileImageUrl(session: OAuthSession, value: unknown): Promise<string | undefined> {
  if (!value || typeof value !== "object" || !("ref" in value)) return undefined;
  const ref = value.ref;
  const cid = ref && typeof ref === "object" && "$link" in ref && typeof ref.$link === "string"
    ? ref.$link : ref && typeof ref === "object" && "toString" in ref ? String(ref) : undefined;
  if (!cid || cid === "[object Object]") return undefined;
  const { aud } = await session.getTokenInfo(false);
  const url = new URL("/xrpc/com.atproto.sync.getBlob", aud);
  if (url.protocol !== "https:" || url.username || url.password) return undefined;
  url.search = new URLSearchParams({ did: session.did, cid }).toString();
  return url.href;
}

/** Load the canonical PDS record, including fields this editor does not modify. */
export async function loadEditableProfile(session: OAuthSession, signal?: AbortSignal): Promise<EditableProfileSnapshot> {
  signal?.throwIfAborted();
  try {
    const { data } = await createOAuthAgent(session).com.atproto.repo.getRecord({ repo: session.did, collection: PROFILE_COLLECTION, rkey: "self" }, { signal });
    signal?.throwIfAborted();
    if (!data.cid || !data.value || typeof data.value !== "object" || Array.isArray(data.value)) throw new Error("The profile record could not be loaded. Please try again.");
    return { record: data.value as Record<string, unknown>, cid: data.cid };
  } catch (error) {
    signal?.throwIfAborted();
    if (errorCode(error) === "RecordNotFound") return { record: { $type: PROFILE_COLLECTION }, cid: null };
    if (isMissingOAuthScope(error)) throw new Error(SCOPE_RECOVERY_MESSAGE);
    throw error;
  }
}

/** A CID compare-and-swap prevents replacing edits made by another client. */
export async function saveEditableProfile(options: SaveEditableProfileOptions): Promise<EditableProfileSnapshot> {
  const { session, expectedDid, snapshot, displayName, description, avatar, banner, signal } = options;
  const guard = () => {
    signal?.throwIfAborted();
    if (session.did !== expectedDid) throw new Error("Your account changed. Please reopen the profile editor.");
    options.assertCurrent?.();
  };
  guard();
  validateProfileText(displayName, description);
  for (const file of [avatar, banner]) if (file) validateProfileImage(file);
  const agent = createOAuthAgent(session);
  const record: Record<string, unknown> = { ...snapshot.record, $type: PROFILE_COLLECTION, displayName, description };
  try {
    for (const [field, file] of [["avatar", avatar], ["banner", banner]] as const) {
      guard();
      if (file === null) delete record[field];
      else if (file !== undefined) {
        const uploaded = await agent.uploadBlob(file, { encoding: file.type, signal });
        guard();
        record[field] = uploaded.data.blob;
      }
    }
    guard();
    const { data } = await agent.com.atproto.repo.putRecord({ repo: expectedDid, collection: PROFILE_COLLECTION, rkey: "self", record, swapRecord: snapshot.cid, validate: true }, { signal });
    guard();
    return { record, cid: data.cid };
  } catch (error) {
    guard();
    if (errorCode(error) === "InvalidSwap") throw new Error("Your profile changed elsewhere. Close and reopen the editor to load the latest version before saving.");
    if (isMissingOAuthScope(error)) throw new Error(SCOPE_RECOVERY_MESSAGE);
    throw error;
  }
}
