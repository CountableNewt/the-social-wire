import * as React from "react"
import { cn } from "@/lib/utils"
export function FieldGroup({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) { return <div className={cn("grid gap-3", className)} {...props} /> }
export function Field({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) { return <div className={cn("grid gap-1", className)} {...props} /> }
export function FieldLabel(props: React.LabelHTMLAttributes<HTMLLabelElement>) { return <label className="text-[11px] font-medium" {...props} /> }
export function FieldDescription(props: React.HTMLAttributes<HTMLParagraphElement>) { return <p className="text-[10px] text-muted-foreground" {...props} /> }
