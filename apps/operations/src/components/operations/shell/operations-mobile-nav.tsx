import Link from "next/link"
import { operationsNav } from "@/components/operations/shell/operations-navigation"

export function MobileOperationsNav({ current }: { current: string }) {
  return (
    <nav aria-label="Mobile Operations" className="flex overflow-x-auto border-b p-2 md:hidden">
      {operationsNav.map(([key, label]) => (
        <Link
          key={key}
          href={key === "overview" ? "/" : `/${key}`}
          className={`rounded-md px-3 py-1.5 text-[11px] ${current === key ? "bg-accent text-accent-foreground" : "text-muted-foreground"}`}
        >
          {label}
        </Link>
      ))}
    </nav>
  )
}
