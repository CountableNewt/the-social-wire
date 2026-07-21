export function OperationsSection({
  title,
  action,
  children,
}: {
  title: React.ReactNode
  action?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="ops-panel min-w-0 w-full max-w-full overflow-hidden">
      <header className="flex min-h-9 items-center justify-between border-b px-3">
        <h2 className="text-xs font-semibold">{title}</h2>
        {action}
      </header>
      {children}
    </section>
  )
}
