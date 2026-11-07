import { SocialVerificationBadge } from "@/components/Social/SocialVerificationBadge";
import type { VerificationSubject } from "@/lib/socialVerification";

export function ProfileDisplayName({ name, pronouns, wrap = false, subject }: { name: string; pronouns?: string; wrap?: boolean; subject?: VerificationSubject }) {
  const text = pronouns?.trim();
  return <span className="inline-flex max-w-full items-center gap-1 align-top">
    <span className={wrap ? "min-w-0" : "min-w-0 truncate"}>{name}</span>
    {subject ? <SocialVerificationBadge subject={subject} /> : null}
    {text ? <> <span className="shrink-0 text-xs leading-none font-normal text-muted-foreground">{text}</span></> : null}
  </span>;
}
