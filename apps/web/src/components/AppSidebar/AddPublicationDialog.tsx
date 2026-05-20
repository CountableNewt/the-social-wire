"use client";

import { useCallback, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { SIDEBAR_GLASS_ROW_ACTION } from "@/components/ui/sidebar";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { CirclePlus } from "lucide-react";
import { cn } from "@/lib/utils";
import { AddPublicationInner } from "./AddPublication/AddPublicationInner";

export function AddPublicationDialog() {
  const [open, setOpen] = useState(false);
  const [formKey, setFormKey] = useState(0);
  const descriptionId = `${useId()}-add-pub-desc`;

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    if (next) setFormKey((k) => k + 1);
  }, []);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button
            variant="ghost"
            className={cn(SIDEBAR_GLASS_ROW_ACTION)}
          />
        }
      >
        <CirclePlus className="h-4 w-4 shrink-0" />
        Add Publication
      </DialogTrigger>
      <DialogContent aria-describedby={descriptionId}>
        <DialogHeader>
          <DialogTitle>Add Publication</DialogTitle>
          <DialogDescription id={descriptionId}>
            Paste any link, a Bluesky handle, a DID, or a publication AT-URI. We look for{" "}
            <code className="text-[10px]">/.well-known/site.standard.publication</code> first, then try
            RSS/Atom and save a Skyreader-compatible feed subscription if needed (
            <code className="text-[10px]">app.skyreader.feed.subscription</code>) on your PDS.
          </DialogDescription>
        </DialogHeader>
        <AddPublicationInner key={formKey} onCloseRequest={() => handleOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}
