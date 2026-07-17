"use client"
import * as React from "react"
import { ResponsiveContainer } from "recharts"
import { cn } from "@/lib/utils"
export function ChartContainer({ className, children }: { className?: string; children: React.ReactElement }) { return <div className={cn("h-12 w-full", className)}><ResponsiveContainer width="100%" height="100%">{children}</ResponsiveContainer></div> }
