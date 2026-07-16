"use client"
import * as React from "react"
type TooltipChildProps = { title?: string; "aria-label"?: string }
export function Tooltip({ label, children }: { label: string; children: React.ReactElement<TooltipChildProps> }) { return React.cloneElement(children, { title: label, "aria-label": children.props["aria-label"] ?? label }) }
