"use client";

import { useId } from "react";
import { Settings2, X } from "lucide-react";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import type { PlayerContext } from "./PodcastPlayerProvider";
import { PodcastSelect } from "./PodcastSelect";
import { PODCAST_SPEEDS } from "@/lib/podcasts/playback";

export function PodcastPlaybackDefaults({ player }: { player: PlayerContext }) {
  const speedId = useId();
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
        <label htmlFor={speedId} className="flex items-center justify-between gap-3 text-sm">Default Speed
          <PodcastSelect id={speedId} value={player.state.playbackSpeed} onChange={event => void player.changeState({ playbackSpeed: Number(event.target.value) })} className="min-h-9 rounded border bg-background px-2 pointer-coarse:min-h-11">
            {PODCAST_SPEEDS.map(speed => <option key={speed} value={speed}>{speed}×</option>)}
          </PodcastSelect>
        </label>
        <label className="flex min-h-9 items-center gap-2 text-sm pointer-coarse:min-h-11"><input type="checkbox" checked={player.state.removeSilences} onChange={event => void player.setRemoveSilences(event.target.checked)} />Remove Silences by Default</label>
        <p className="text-xs text-muted-foreground">Silence removal is unavailable for private feeds.</p>
        <button type="button" className="min-h-8 rounded border px-2 text-xs hover:bg-accent pointer-coarse:min-h-11" onClick={() => void player.changeState({ playbackSpeed: 1, removeSilences: false })}>Reset Defaults</button>
    </DialogContent>
  </Dialog>;
}
