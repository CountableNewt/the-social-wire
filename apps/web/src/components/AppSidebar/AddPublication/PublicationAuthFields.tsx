"use client";

import { type FormEvent, type ReactNode, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DialogClose,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useAuth } from "@/hooks/useAuth";
import { Loader2 } from "lucide-react";

export interface PublicationAuthFieldsProps {
  idPrefix: string;
  lead: ReactNode | null;
  seedHandle: string;
}

export function PublicationAuthFields({
  idPrefix,
  lead,
  seedHandle,
}: PublicationAuthFieldsProps) {
  const { signIn } = useAuth();
  const handleId = `${idPrefix}-authorize-handle`;

  const [handle, setHandle] = useState(seedHandle);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    const h = handle.trim();
    if (!h) {
      setError("Enter your handle to continue.");
      return;
    }
    setBusy(true);
    try {
      await signIn(h);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Authorization failed. Try again."
      );
      setBusy(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4">
      {lead ? (
        <div className="space-y-2 text-sm text-muted-foreground">{lead}</div>
      ) : null}
      <div className="space-y-1.5">
        <Label htmlFor={handleId}>Bluesky or ATProto handle</Label>
        <Input
          id={handleId}
          type="text"
          name="publication-authorize-handle"
          autoCapitalize="none"
          autoCorrect="off"
          autoComplete="username"
          spellCheck={false}
          placeholder="you.bsky.social"
          value={handle}
          onChange={(ev) => setHandle(ev.target.value)}
          disabled={busy}
          required
        />
      </div>
      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <DialogFooter className="gap-2 sm:gap-0">
        <DialogClose
          disabled={busy}
          render={<Button type="button" variant="outline" disabled={busy} />}
        >
          Cancel
        </DialogClose>
        <Button type="submit" disabled={busy || !handle.trim()}>
          {busy ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" aria-hidden />
              Redirecting…
            </>
          ) : (
            "Authorize Publication Subscriptions"
          )}
        </Button>
      </DialogFooter>
      <p className="text-xs text-muted-foreground">
        Signing in grants repository access including{" "}
        <code className="text-[10px]">site.standard.graph.subscription</code> and{" "}
        <code className="text-[10px]">app.skyreader.feed.subscription</code>.
      </p>
    </form>
  );
}
