"use client";

import { BadgeCheck, CircleCheck } from "lucide-react";
import { useSocialVerification } from "@/contexts/SocialVerificationContext";
import { verificationLabel, type VerificationSubject } from "@/lib/socialVerification";

export function SocialVerificationBadge({ subject }: { subject: VerificationSubject }) {
  const { preferences, records } = useSocialVerification();
  const label = verificationLabel(subject, preferences, records);
  if (!label) return null;
  const Icon = label === "Trusted Verifier on Bluesky" ? BadgeCheck : CircleCheck;
  return <span role="img" aria-label={label} title={label} className="inline-flex shrink-0 text-primary"><Icon aria-hidden="true" className="size-4" /></span>;
}
