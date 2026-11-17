"use client";

import { Settings2, X } from "lucide-react";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import type { PlayerContext } from "./PodcastPlayerProvider";
import { PodcastPlaybackDefaultsFields } from "./PodcastPlaybackDefaultsFields";

export function PodcastPlaybackDefaults({ player }: { player: PlayerContext }) {
  return <Dialog>
    <DialogTrigger aria-label="Playback Defaults" title="Playback Defaults" className="flex min-h-8 min-w-8 items-center justify-center rounded border hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-11 pointer-coarse:min-w-11">
      <Settings2 aria-hidden="true" className="size-3.5" />
    </DialogTrigger>
    <DialogContent showCloseButton={false}>
        <DialogHeader>
          <div className="flex items-center justify-between gap-2"><DialogTitle>Playback Defaults</DialogTitle>
            <DialogClose aria-label="Close Playback Defaults" className="flex min-h-8 min-w-8 items-center justify-center rounded hover:bg-accent pointer-coarse:min-h-11 pointer-coarse:min-w-11"><X aria-hidden="true" className="size-3.5" /></DialogClose>
          </div>
          <DialogDescription>Saved for all podcasts. Changes also apply to current playback.</DialogDescription>
        </DialogHeader>
        <PodcastPlaybackDefaultsFields player={player} />
    </DialogContent>
  </Dialog>;
}
