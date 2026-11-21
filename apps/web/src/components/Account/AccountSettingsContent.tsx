"use client";

import { useState } from "react";
import { useAuth } from "@/hooks/useAuth";
import { useViewerProfile } from "@/hooks/useViewerProfile";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { TrustedVerifiersSettingsSection } from "./TrustedVerifiersSettingsSection";
import { FeedSettingsSection } from "./FeedSettingsSection";
import { OpmlImportSection } from "./OpmlImportSection";
import { SocialModerationSettingsSection } from "./SocialModerationSettingsSection";
import { ProfileEditDialog } from "./ProfileEditDialog";

export function AccountSettingsContent() {
  const { session, signOut } = useAuth();
  const profile = useViewerProfile();
  const [confirming, setConfirming] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const handle = profile.data?.handle;

  const logOut = async () => {
    setSigningOut(true);
    setError(null);
    try {
      await signOut();
      setConfirming(false);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not log out. Try again.");
    } finally {
      setSigningOut(false);
    }
  };

  return <div className="flex flex-col">
    <section aria-labelledby="account-settings-heading" className="p-4 md:p-6">
      <div className="mx-auto flex max-w-2xl flex-wrap items-center gap-x-3 gap-y-2">
        <h2 id="account-settings-heading" className="text-sm font-bold">Account</h2>
        <p className="min-w-0 break-all text-sm text-muted-foreground">{handle ? (handle.startsWith("did:") ? handle : `@${handle}`) : session?.did}</p>
        <div className="ml-auto flex items-center gap-2">
          <ProfileEditDialog />
          <Button variant="destructive" disabled={!session || signingOut} onClick={() => { setError(null); setConfirming(true); }}>Log Out</Button>
        </div>
      </div>
    </section>
    <FeedSettingsSection />
    <SocialModerationSettingsSection />
    <TrustedVerifiersSettingsSection />
    <OpmlImportSection />
    <Dialog open={confirming} onOpenChange={(open) => { if (!signingOut) setConfirming(open); }}>
      <DialogContent showCloseButton={!signingOut}>
        <DialogHeader>
          <DialogTitle>Log Out of The Social Wire?</DialogTitle>
          <DialogDescription>You&apos;ll need to sign in again to see your feeds.</DialogDescription>
        </DialogHeader>
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        <DialogFooter>
          <DialogClose disabled={signingOut} render={<Button variant="outline" />}>Cancel</DialogClose>
          <Button variant="destructive" disabled={signingOut} onClick={() => { void logOut(); }}>{signingOut ? "Logging Out…" : "Log Out"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>;
}
