"use client";

import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSocialVerification } from "@/contexts/SocialVerificationContext";
import { resolveVerifierProfile } from "@/lib/socialVerification";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { TrustedVerifierRow } from "./TrustedVerifierRow";

export function TrustedVerifiersSettingsSection() {
  const { preferences, update, available, loading, error: requestError, retry } = useSocialVerification();
  const queryClient = useQueryClient();
  const [actor, setActor] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [removing, setRemoving] = useState<string>();
  const lock = useRef(false);
  const change = (action: () => void) => {
    try { action(); setError(undefined); } catch (failure) { setError(failure instanceof Error ? failure.message : "Could Not Save Trusted Verifiers."); }
  };
  async function add() {
    if (lock.current || !actor.trim()) return;
    lock.current = true; setPending(true); setError(undefined);
    try {
      const profile = await resolveVerifierProfile(actor);
      update(previous => ({ ...previous, trustedVerifierDIDs: [...new Set([...previous.trustedVerifierDIDs, profile.did])] }));
      queryClient.setQueryData(["verificationIssuerProfile", profile.did], profile); setActor("");
    } catch (failure) { setError(failure instanceof Error ? failure.message : "Could Not Resolve That Verifier."); }
    finally { lock.current = false; setPending(false); }
  }
  return <section aria-labelledby="trusted-verifiers-heading" className="border-t p-4 md:p-6">
    <div className="mx-auto flex max-w-2xl flex-col gap-3">
      <h2 id="trusted-verifiers-heading" className="text-sm font-bold">Trusted Verifiers</h2>
      <p className="text-sm text-muted-foreground">Show Bluesky verification badges and trust additional accounts that issue verifications. These settings apply to this account in this browser.</p>
      <div className="flex min-h-12 items-center justify-between gap-4 text-sm font-medium"><span>Bluesky Verification</span><Switch aria-label="Bluesky Verification" checked={preferences.includesBlueskyVerification} disabled={!available || pending} onCheckedChange={checked => change(() => update(previous => ({ ...previous, includesBlueskyVerification: checked })))} /></div>
      <form className="flex flex-wrap items-end gap-2" onSubmit={event => { event.preventDefault(); void add(); }}>
        <label className="min-w-0 flex-1 space-y-1 text-sm"><span>Verifier Handle or DID</span><Input aria-label="Verifier Handle or DID" placeholder="@verifier.example" autoCapitalize="none" autoCorrect="off" value={actor} disabled={!available || pending} onChange={event => setActor(event.target.value)} /></label>
        <Button type="submit" variant="outline" disabled={!available || pending || !actor.trim()}>{pending ? "Adding…" : "Add Verifier"}</Button>
      </form>
      <p className="text-xs text-muted-foreground">Trusting an account displays badges only for its valid verification records. It does not verify that account or change what others see.</p>
      {preferences.trustedVerifierDIDs.length ? <ul className="space-y-2">{preferences.trustedVerifierDIDs.map(did => <TrustedVerifierRow key={did} did={did} disabled={pending} onRemove={() => setRemoving(did)} />)}</ul> : <p className="text-sm text-muted-foreground">No Custom Verifiers.</p>}
      {loading ? <p role="status" className="text-xs text-muted-foreground">Loading Custom Verifications…</p> : null}
      {error || requestError ? <div role="alert" className="text-sm text-destructive">{error ?? requestError}{requestError ? <Button variant="outline" size="sm" className="ml-2" onClick={retry}>Retry Verifications</Button> : null}</div> : null}
    </div>
    <Dialog open={!!removing} onOpenChange={open => { if (!open) setRemoving(undefined); }}>
      <DialogContent><DialogTitle>Remove Trusted Verifier?</DialogTitle><DialogDescription>Badges issued only by this verifier will no longer appear for you.</DialogDescription><div className="flex gap-2"><Button variant="destructive" onClick={() => change(() => { update(previous => ({ ...previous, trustedVerifierDIDs: previous.trustedVerifierDIDs.filter(did => did !== removing) })); setRemoving(undefined); })}>Remove Verifier</Button><DialogClose render={<Button variant="outline" />}>Cancel</DialogClose></div></DialogContent>
    </Dialog>
  </section>;
}
