"use client";

import { useState } from "react";
import type { AppBskyFeedDefs } from "@atproto/api";
import { useBrandAccounts } from "@/hooks/useBrandAccounts";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { AccountReplyContent } from "./AccountReplyContent";
import { SocialComposeAccountPicker } from "./SocialComposeAccountPicker";

export function AccountReplyDialog({ ownerDid, recipientDid, parent, close }: {
  ownerDid: string;
  recipientDid: string;
  parent: AppBskyFeedDefs.PostView;
  close: () => void;
}) {
  const accounts = useBrandAccounts(ownerDid);
  const [actorDid, setActorDid] = useState(recipientDid);
  const [text, setText] = useState("");
  const [pending, setPending] = useState(false);
  const available = actorDid === ownerDid || accounts.includes(actorDid);
  return <Dialog open onOpenChange={open => { if (!open && !pending) close(); }}>
    <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl" showCloseButton={!pending}>
      <DialogTitle>Reply</DialogTitle>
      <DialogDescription className="sr-only">Choose an account to post this reply.</DialogDescription>
      <SocialComposeAccountPicker label="Reply As" ownerDid={ownerDid} accounts={accounts} actorDid={actorDid} disabled={pending} onChange={setActorDid} />
      {available ? <AccountReplyContent key={actorDid} ownerDid={ownerDid} actorDid={actorDid} parent={parent} draft={{ text, setText }}
        pending={pending} setPending={setPending} close={close} /> : <p role="alert">Choose a linked account to reply.</p>}
    </DialogContent>
  </Dialog>;
}
