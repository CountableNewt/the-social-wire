"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/hooks/useAuth";
import { useViewerProfile, VIEWER_PROFILE_QUERY_KEY } from "@/hooks/useViewerProfile";
import { editableProfileImageUrl, loadEditableProfile, saveEditableProfile, type EditableProfileSnapshot } from "@/lib/profileEditing";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { ProfileImageField } from "./ProfileImageField";

export function ProfileEditDialog() {
  const { session } = useAuth();
  return <ProfileEditForm key={session?.did ?? ""} />;
}

function ProfileEditForm() {
  const nameId = useId();
  const bioId = useId();
  const { session, getOAuthSession } = useAuth();
  const profile = useViewerProfile();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [snapshot, setSnapshot] = useState<EditableProfileSnapshot>();
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [avatar, setAvatar] = useState<File | null>();
  const [banner, setBanner] = useState<File | null>();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [saved, setSaved] = useState(false);
  const current = useRef({ did: session?.did, getOAuthSession });
  useEffect(() => { current.current = { did: session?.did, getOAuthSession }; }, [session?.did, getOAuthSession]);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);

  async function load() {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true); setSnapshot(undefined); setError(undefined);
    setDisplayName(""); setDescription(""); setAvatar(undefined); setBanner(undefined);
    try {
      const oauth = current.current.getOAuthSession();
      if (!oauth || oauth.did !== current.current.did) throw new Error("Please Log In Again to Edit Your Profile.");
      const result = await loadEditableProfile(oauth, controller.signal);
      controller.signal.throwIfAborted();
      if (oauth.did !== current.current.did) throw new Error("Your Account Changed. Reopen the Profile Editor.");
      setSnapshot(result);
      setDisplayName(typeof result.record.displayName === "string" ? result.record.displayName : "");
      setDescription(typeof result.record.description === "string" ? result.record.description : "");
    } catch (reason) {
      if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Could Not Load Your Profile.");
    } finally { if (!controller.signal.aborted) setLoading(false); }
  }
  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!snapshot || saving) return;
    const oauth = current.current.getOAuthSession();
    const did = current.current.did;
    if (!oauth || !did || oauth.did !== did) { setError("Please Log In Again to Edit Your Profile."); return; }
    const controller = new AbortController();
    request.current?.abort(); request.current = controller;
    const assertCurrent = () => {
      controller.signal.throwIfAborted();
      if (current.current.did !== did || current.current.getOAuthSession() !== oauth) throw new Error("Your Account Changed. Reopen the Profile Editor.");
    };
    setSaving(true); setError(undefined);
    try {
      const result = await saveEditableProfile({ session: oauth, expectedDid: did, snapshot, displayName, description, avatar, banner, assertCurrent, signal: controller.signal });
      assertCurrent();
      const [avatarUrl, bannerUrl] = await Promise.all([
        avatar ? editableProfileImageUrl(oauth, result.record.avatar).catch(() => undefined) : undefined,
        banner ? editableProfileImageUrl(oauth, result.record.banner).catch(() => undefined) : undefined,
      ]);
      assertCurrent();
      await client.cancelQueries({ queryKey: VIEWER_PROFILE_QUERY_KEY(did), exact: true });
      assertCurrent();
      // Keep freshly saved text visible while AppView catches up to the PDS write.
      client.setQueryData(VIEWER_PROFILE_QUERY_KEY(did), { ...profile.data, did, displayName, description, ...(avatar !== undefined ? { avatar: avatarUrl } : {}), ...(banner !== undefined ? { banner: bannerUrl } : {}) });
      void client.invalidateQueries({ queryKey: ["blueskySocial", did] });
      void client.invalidateQueries({ queryKey: VIEWER_PROFILE_QUERY_KEY(did), refetchType: "none" });
      setSaved(true); setOpen(false);
    } catch (reason) {
      if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Could Not Save Your Profile.");
    } finally { if (!controller.signal.aborted) setSaving(false); }
  }
  return <div className="space-y-2">
    <Dialog open={open} onOpenChange={next => {
      if (saving) return;
      setOpen(next);
      if (next) { setSaved(false); void load(); } else request.current?.abort();
    }}>
      <DialogTrigger render={<Button variant="outline" />} disabled={!session}>Edit Profile</DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto" showCloseButton={!saving}>
        <DialogHeader><DialogTitle>Edit Profile</DialogTitle><DialogDescription>Update your public Bluesky profile.</DialogDescription></DialogHeader>
        {loading ? <p role="status">Loading Profile…</p> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {!loading && !snapshot ? <Button variant="outline" onClick={() => void load()}>Retry Profile</Button> : null}
        {snapshot ? <form onSubmit={event => void save(event)} className="min-w-0 space-y-4">
          <fieldset disabled={saving} className="min-w-0 space-y-4">
            <div className="space-y-1"><label htmlFor={nameId} className="block text-sm font-medium">Display Name</label><input id={nameId} value={displayName} onChange={event => setDisplayName(event.target.value)} className="w-full rounded-lg border bg-background px-3 py-2" /></div>
            <div className="space-y-1"><label htmlFor={bioId} className="block text-sm font-medium">Bio</label><textarea id={bioId} value={description} onChange={event => setDescription(event.target.value)} rows={4} className="w-full resize-y rounded-lg border bg-background px-3 py-2" /></div>
            <p className="text-xs text-muted-foreground">Display name: up to 64 characters. Bio: up to 256 characters. Images: JPEG or PNG, up to 1 MB each.</p>
            <ProfileImageField label="Avatar" currentUrl={profile.data?.avatar} hasImage={!!snapshot.record.avatar} value={avatar} onChange={setAvatar} disabled={saving} />
            <ProfileImageField label="Banner" currentUrl={profile.data?.banner} hasImage={!!snapshot.record.banner} value={banner} onChange={setBanner} disabled={saving} />
          </fieldset>
          <DialogFooter><DialogClose render={<Button variant="outline" />} disabled={saving}>Cancel</DialogClose><Button type="submit" disabled={saving}>{saving ? "Saving…" : "Save Profile"}</Button></DialogFooter>
        </form> : null}
      </DialogContent>
    </Dialog>
    {saved ? <p role="status" className="text-xs text-muted-foreground">Profile Saved.</p> : null}
  </div>;
}
