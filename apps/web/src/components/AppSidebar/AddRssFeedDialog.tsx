"use client";

import Link from "next/link";
import {
  type FormEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useState,
} from "react";
import { useRouter } from "next/navigation";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useAuth } from "@/hooks/useAuth";
import { useCreateSkyreaderFeedSubscription } from "@/hooks/usePublications";
import { usePDSClient } from "@/hooks/usePDSClient";
import { useViewerProfile } from "@/hooks/useViewerProfile";
import { Loader2, Rss } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  rssPublicationIdFromNormalizedFeedUrl,
  normalizeRssFeedUrlInput,
} from "@/lib/rssFeedCore";

/** True when RSS subscription write likely failed for auth / scope reasons. */
function looksLikeOAuthScopeOrSessionError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  const msg = `${error.message} ${String(error.cause ?? "")}`.toLowerCase();
  if (msg.includes("no pds client")) return true;
  if (/\b401\b|\b403\b|\binvalid token\b|\bexpired\b/.test(msg)) return true;
  if (/\bunauthorized\b|\bforbidden\b|\bpermission\b|\bscope\b/.test(msg)) {
    return true;
  }
  const anyErr = error as { status?: number };
  const st = typeof anyErr.status === "number" ? anyErr.status : undefined;
  if (st === 401 || st === 403) return true;
  return false;
}

export function AddRssFeedDialog() {
  const [open, setOpen] = useState(false);
  const [formKey, setFormKey] = useState(0);
  const descriptionId = `${useId()}-rss-desc`;

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    if (next) setFormKey((k) => k + 1);
  }, []);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button variant="ghost" size="sm" className="w-full justify-start gap-2" />
        }
      >
        <Rss className="h-4 w-4" />
        Add RSS Feed
      </DialogTrigger>
      {/* Keep Dialog.Portal/Popup outside the keyed subtree so Base UI open/close stays in sync */}
      <DialogContent aria-describedby={descriptionId}>
        <DialogHeader>
          <DialogTitle>Add RSS Feed</DialogTitle>
          <DialogDescription id={descriptionId}>
            Subscribe to any public RSS or Atom feed. A Skyreader-compatible record (
            <code className="text-[10px]">app.skyreader.feed.subscription</code>) is saved on your
            PDS.
          </DialogDescription>
        </DialogHeader>
        <AddRssFeedInner key={formKey} onCloseRequest={() => handleOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

interface AddRssFeedInnerProps {
  onCloseRequest: () => void;
}

interface SkyreaderSignInFieldsProps {
  idPrefix: string;
  /** Narrow explainer shown above the handle field */
  lead: ReactNode | null;
}

function SkyreaderSignInFields({ idPrefix, lead }: SkyreaderSignInFieldsProps) {
  const { signIn } = useAuth();
  const { data: profile } = useViewerProfile();
  const handleId = `${idPrefix}-skyreader-handle`;

  const [handle, setHandle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const h = profile?.handle?.trim();
    if (h && !h.startsWith("did:")) {
      setHandle((prev) => (prev.trim() === "" ? h : prev));
    }
  }, [profile?.handle]);

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
          name="skyreader-handle"
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
            "Authorize RSS (Skyreader) access"
          )}
        </Button>
      </DialogFooter>
      <p className="text-xs text-muted-foreground">
        Signing in sends you to your PDS to approve repository access including{" "}
        <code className="text-[10px]">app.skyreader.feed.subscription</code>. Use the same account
        you use for The Social Wire.
      </p>
    </form>
  );
}

function AddRssFeedInner({ onCloseRequest }: AddRssFeedInnerProps) {
  const labelId = useId();
  const urlId = `${labelId}-url`;
  const titleId = `${labelId}-title`;
  const router = useRouter();
  const { session, isLoading: authLoading } = useAuth();
  const client = usePDSClient();

  const [feedUrl, setFeedUrl] = useState("");
  const [title, setTitle] = useState("");
  const [finishing, setFinishing] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [showScopeReconnect, setShowScopeReconnect] = useState(false);

  const createSkyreader = useCreateSkyreaderFeedSubscription();

  const oauthReady = !!client;
  const signedOut = !session?.did && !authLoading;
  const sessionPendingOAuth =
    !!session?.did && !oauthReady && !authLoading;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!feedUrl.trim() || finishing) return;
    setSubmitError(null);
    setShowScopeReconnect(false);
    const normalized = normalizeRssFeedUrlInput(feedUrl);
    if (!normalized) {
      setSubmitError("Enter a valid http(s) URL.");
      return;
    }

    setFinishing(true);
    try {
      await createSkyreader.mutateAsync({
        feedUrl: normalized,
        title: title.trim() || undefined,
      });
      const pubId = rssPublicationIdFromNormalizedFeedUrl(normalized);
      onCloseRequest();
      router.push(`/read/${encodeURIComponent(pubId)}`);
    } catch (err) {
      console.error(err);
      if (looksLikeOAuthScopeOrSessionError(err)) {
        setShowScopeReconnect(true);
        setSubmitError(
          "Authorization issue — sign in again to allow RSS subscriptions on your PDS."
        );
      } else {
        setSubmitError(
          err instanceof Error ? err.message : "Something went wrong. Try again."
        );
      }
    } finally {
      setFinishing(false);
    }
  }

  const pending = finishing || createSkyreader.isPending;

  return (
    <>
      {authLoading ? (
        <div className="space-y-4 py-2">
          <div
            className="flex flex-col items-center gap-3 py-4 text-center text-sm text-muted-foreground"
            role="status"
          >
            <Loader2 className="h-8 w-8 animate-spin opacity-70" aria-hidden />
            Checking your ATProto session…
          </div>
          <div className="flex justify-end">
            <DialogClose render={<Button type="button" variant="outline" />}>
              Cancel
            </DialogClose>
          </div>
        </div>
      ) : signedOut ? (
        <div className="space-y-4 py-2">
          <p className="text-sm text-muted-foreground">
            Sign in with your Bluesky or ATProto account so we can write Skyreader-compatible RSS
            subscriptions to your PDS.
          </p>
          <div className="flex flex-wrap gap-2">
            <Link href="/login" className={cn(buttonVariants({ variant: "default" }))}>
              Sign in to continue
            </Link>
            <DialogClose render={<Button type="button" variant="outline" />}>
              Cancel
            </DialogClose>
          </div>
        </div>
      ) : sessionPendingOAuth ? (
        <SkyreaderSignInFields
          idPrefix={labelId}
          lead={
            <p>
              Your account is remembered, but this browser does not have an active ATProto OAuth
              session. Authorize below to reconnect and save RSS feeds.
            </p>
          }
        />
      ) : (
        <>
          {showScopeReconnect ? (
            <div className="rounded-md border border-border bg-muted/40 p-3">
              <p className="text-sm font-medium text-foreground">Skyreader authorization</p>
              <p className="mt-1 text-xs text-muted-foreground">
                If you joined before RSS support was added, your saved login may omit the RSS
                collection scope. Signing in again refreshes permission for{" "}
                <code className="text-[10px]">app.skyreader.feed.subscription</code>.
              </p>
              <div className="mt-3">
                <SkyreaderSignInFields idPrefix={`${labelId}-re`} lead={null} />
              </div>
            </div>
          ) : null}
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor={urlId}>Feed URL</Label>
              <Input
                id={urlId}
                type="url"
                inputMode="url"
                placeholder="https://example.com/feed.xml"
                value={feedUrl}
                onChange={(e) => setFeedUrl(e.target.value)}
                autoFocus={!showScopeReconnect}
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={titleId}>Title (optional)</Label>
              <Input
                id={titleId}
                placeholder="Friendly name shown in the sidebar"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
            </div>
            {submitError ? (
              <p className="text-sm text-destructive" role="alert">
                {submitError}
              </p>
            ) : null}
            <DialogFooter>
              <DialogClose
                disabled={pending}
                render={<Button type="button" variant="outline" disabled={pending} />}
              >
                Cancel
              </DialogClose>
              <button
                type="submit"
                disabled={!feedUrl.trim() || pending}
                className={cn(buttonVariants())}
              >
                {pending ? "Saving…" : "Subscribe"}
              </button>
            </DialogFooter>
          </form>
        </>
      )}
    </>
  );
}
