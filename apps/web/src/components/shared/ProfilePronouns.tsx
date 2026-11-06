export function ProfilePronouns({ pronouns }: { pronouns?: string }) {
  const text = pronouns?.trim();
  return text ? <> <sub className="align-sub text-xs font-normal text-muted-foreground">{text}</sub></> : null;
}
