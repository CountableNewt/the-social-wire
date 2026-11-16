"use client";

import { BadgeCheck, CircleCheck } from "lucide-react";
import { useId, useState } from "react";
import { useSocialVerification } from "@/contexts/SocialVerificationContext";
import { verificationIssuers, verificationLabel, type VerificationSubject } from "@/lib/socialVerification";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SocialVerifierIdentity } from "./SocialVerifierIdentity";

export function SocialVerificationBadge({ subject }: { subject: VerificationSubject }) {
  const tooltipId = useId();
  const [open, setOpen] = useState(false);
  const { preferences, records } = useSocialVerification();
  const label = verificationLabel(subject, preferences, records);
  if (!label) return null;
  const issuers = verificationIssuers(subject, preferences, records);
  const Icon = label === "Trusted Verifier on Bluesky" ? BadgeCheck : CircleCheck;
  return <Tooltip open={open} onOpenChange={setOpen}>
    <TooltipTrigger delay={150} render={<span role="img" tabIndex={0} aria-label={label} aria-describedby={open ? tooltipId : undefined} className="inline-flex shrink-0 rounded-sm text-primary focus-visible:outline-2 focus-visible:outline-ring" />}>
      <Icon aria-hidden="true" className="size-4" />
    </TooltipTrigger>
    <TooltipContent id={tooltipId} role="tooltip" className="block max-w-[min(20rem,calc(100vw-2rem))] whitespace-normal">
      <p className="font-semibold">{label}</p>
      {issuers.length ? <><p className="mt-1">Verified By</p><ul className="mt-1 space-y-1">{issuers.map(did => <SocialVerifierIdentity key={did} did={did} />)}</ul></> : <p className="mt-1">{label === "Trusted Verifier on Bluesky" ? "Recognized as a Trusted Verifier by Bluesky." : "Verifier Details Are Unavailable."}</p>}
    </TooltipContent>
  </Tooltip>;
}
