import * as React from "react"
import { ChevronDown } from "lucide-react"
import { cn } from "@/lib/utils"
export function Select({ className, children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) { return <span className="relative inline-flex"><select className={cn("h-8 appearance-none rounded-md border bg-background py-0 pl-2.5 pr-7 text-xs", className)} {...props}>{children}</select><ChevronDown aria-hidden className="pointer-events-none absolute right-2 top-2.5 size-3 text-muted-foreground" /></span> }
