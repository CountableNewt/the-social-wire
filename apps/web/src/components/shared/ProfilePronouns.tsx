export function ProfilePronouns({ pronouns }: { pronouns?: string }) {
  const text = pronouns?.trim();
  return text ? <> <sub className="static align-sub text-xs leading-[0] font-normal text-muted-foreground">{text}</sub></> : null;
}
