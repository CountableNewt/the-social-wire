import * as React from "react"
import { cn } from "@/lib/utils"
export function Alert({ className, variant = "default", ...props }: React.HTMLAttributes<HTMLDivElement> & { variant?: "default" | "warning" | "destructive" }) { return <div role="alert" className={cn("rounded-md border p-3 text-xs", variant === "warning" && "border-amber-300 bg-amber-50 text-amber-900", variant === "destructive" && "border-red-300 bg-red-50 text-red-900", className)} {...props} /> }
export function AlertTitle(props: React.HTMLAttributes<HTMLHeadingElement>) { return <h3 className="font-semibold" {...props} /> }
export function AlertDescription(props: React.HTMLAttributes<HTMLParagraphElement>) { return <p className="mt-1 text-current/80" {...props} /> }
