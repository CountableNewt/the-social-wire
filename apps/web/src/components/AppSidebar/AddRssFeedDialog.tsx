"use client";

import { type ReactNode, useCallback, useId, useState } from "react";
import { useRouter } from "next/navigation";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCreateSkyreaderFeedSubscription } from "@/hooks/usePublications";
import { Rss } from "lucide-react";
import { cn } from "@/lib/utils";
import { rssPublicationIdFromNormalizedFeedUrl, normalizeRssFeedUrlInput } from "@/lib/rssFeedCore";

export function AddRssFeedDialog() {
  const [open, setOpen] = useState(false);
  const [formKey, setFormKey] = useState(0);

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    if (!next) setFormKey((k) => k + 1);
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
      <AddRssFeedFormFields
        key={formKey}
        onOpenChange={handleOpenChange}
        description={
          <>
            Subscribe to any public RSS or Atom feed. A Skyreader-compatible record{" "}
            (<code className="text-[10px]">app.skyreader.feed.subscription</code>) is saved on your
            PDS.
          </>
        }
      />
    </Dialog>
  );
}

interface AddRssFeedFormFieldsProps {
  onOpenChange: (open: boolean) => void;
  description?: ReactNode;
}

function AddRssFeedFormFields({
  onOpenChange,
  description,
}: AddRssFeedFormFieldsProps) {
  const labelId = useId();
  const descId = `${labelId}-desc`;
  const urlId = `${labelId}-url`;
  const titleId = `${labelId}-title`;
  const router = useRouter();

  const [feedUrl, setFeedUrl] = useState("");
  const [title, setTitle] = useState("");
  const [finishing, setFinishing] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const createSkyreader = useCreateSkyreaderFeedSubscription();

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!feedUrl.trim() || finishing) return;
    setSubmitError(null);
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
      onOpenChange(false);
      router.push(`/read/${encodeURIComponent(pubId)}`);
    } catch (err) {
      console.error(err);
      setSubmitError(
        err instanceof Error ? err.message : "Something went wrong. Try again."
      );
    } finally {
      setFinishing(false);
    }
  }

  const pending = finishing || createSkyreader.isPending;

  return (
    <DialogContent aria-describedby={description ? descId : undefined}>
      <DialogHeader>
        <DialogTitle>Add RSS Feed</DialogTitle>
        {description ? (
          <DialogDescription id={descId}>{description}</DialogDescription>
        ) : null}
      </DialogHeader>
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
            autoFocus
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
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <button
            type="submit"
            disabled={!feedUrl.trim() || pending}
            className={cn(buttonVariants())}
          >
            {pending ? "Saving…" : "Subscribe"}
          </button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}
