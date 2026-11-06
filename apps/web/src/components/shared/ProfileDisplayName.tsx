export function ProfileDisplayName({ name, pronouns, wrap = false }: { name: string; pronouns?: string; wrap?: boolean }) {
  const text = pronouns?.trim();
  return <span className="inline-flex max-w-full items-center gap-1 align-top">
    <span className={wrap ? "min-w-0" : "min-w-0 truncate"}>{name}</span>
    {text ? <> <span className="shrink-0 text-xs leading-none font-normal text-muted-foreground">{text}</span></> : null}
  </span>;
}
