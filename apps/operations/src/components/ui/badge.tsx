import * as React from "react"
import { cn } from "@/lib/utils"

export function Badge({ className, tone = "neutral", ...props }: React.HTMLAttributes<HTMLSpanElement> & { tone?: "neutral" | "success" | "warning" | "danger" | "info" }) {
  return <span className={cn(
    "inline-flex items-center rounded-sm border px-1.5 py-0.5 text-[10px] font-medium",
    tone === "success" && "border-emerald-200 bg-emerald-50 text-emerald-700",
    tone === "warning" && "border-amber-200 bg-amber-50 text-amber-700",
    tone === "danger" && "border-red-200 bg-red-50 text-red-700",
    tone === "info" && "border-blue-200 bg-blue-50 text-blue-700",
    tone === "neutral" && "bg-muted text-muted-foreground",
    className
  )} {...props} />
}
