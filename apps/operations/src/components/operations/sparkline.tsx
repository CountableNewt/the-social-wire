"use client"

import { useState, type KeyboardEvent, type MouseEvent, type PointerEvent } from "react"
import { Tooltip } from "@/components/ui/tooltip"
import type { MetricPoint } from "@/lib/collection-metrics"
import { sparklineGeometry } from "@/lib/sparkline-path"

const WIDTH = 80
const HEIGHT = 20

function formatTimestamp(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(timestamp)
}

export function Sparkline({
  points,
  label,
  format,
  tone = "primary",
}: {
  points: MetricPoint[]
  label: string
  format: (value: number) => string
  tone?: "primary" | "warning"
}) {
  const geometry = sparklineGeometry(points, WIDTH, HEIGHT)
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const latestPoint = geometry.points.at(-1)
  const activePoint = activeIndex === null ? undefined : geometry.points[activeIndex]
  const tooltipPoint = activePoint ?? latestPoint
  const stroke = tone === "warning" ? "var(--warning)" : "var(--primary)"

  const selectNearestPoint = (clientX: number, element: HTMLElement) => {
    const bounds = element.getBoundingClientRect()
    if (bounds.width === 0) return

    const chartX = ((clientX - bounds.left) / bounds.width) * WIDTH
    const nearestIndex = geometry.points.reduce(
      (nearest, point, index) =>
        Math.abs(point.x - chartX) < Math.abs(geometry.points[nearest]!.x - chartX) ? index : nearest,
      0,
    )
    setActiveIndex(nearestIndex)
  }

  const handleMousePosition = (event: MouseEvent<HTMLSpanElement>) => {
    selectNearestPoint(event.clientX, event.currentTarget)
  }

  const handlePointerDown = (event: PointerEvent<HTMLSpanElement>) => {
    selectNearestPoint(event.clientX, event.currentTarget)
    event.currentTarget.focus()
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLSpanElement>) => {
    if (event.key === "Escape") {
      setActiveIndex(null)
      event.currentTarget.blur()
      return
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return

    event.preventDefault()
    const fallbackIndex = geometry.points.length - 1
    const currentIndex = activeIndex ?? fallbackIndex
    const direction = event.key === "ArrowLeft" ? -1 : 1
    setActiveIndex(Math.min(Math.max(currentIndex + direction, 0), fallbackIndex))
  }

  return (
    <Tooltip
      label={
        tooltipPoint ? (
          <span className="flex items-center gap-2 font-mono tabular-nums">
            <strong className="font-medium text-popover-foreground">{format(tooltipPoint.value)}</strong>
            <time className="text-muted-foreground" dateTime={new Date(tooltipPoint.timestamp).toISOString()}>
              {formatTimestamp(tooltipPoint.timestamp)}
            </time>
          </span>
        ) : (
          "No history"
        )
      }
    >
      <span
        className="inline-flex h-5 w-20 cursor-crosshair touch-manipulation rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
        role="img"
        aria-label={label}
        tabIndex={0}
        onBlur={() => setActiveIndex(null)}
        onFocus={() => setActiveIndex(geometry.points.length - 1)}
        onKeyDown={handleKeyDown}
        onMouseEnter={handleMousePosition}
        onMouseLeave={() => setActiveIndex(null)}
        onMouseMove={handleMousePosition}
        onPointerDown={handlePointerDown}
      >
        <svg className="h-5 w-20" viewBox={`0 0 ${WIDTH} ${HEIGHT}`} aria-hidden="true">
          {geometry.paths.map((path) => (
            <path key={path} d={path} fill="none" stroke={stroke} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
          ))}
          {geometry.points.length === 1 ? (
            <circle cx={geometry.points[0]!.x} cy={geometry.points[0]!.y} r="2" fill={stroke} />
          ) : null}
          {activePoint ? (
            <>
              <line
                x1={activePoint.x}
                x2={activePoint.x}
                y1="1"
                y2={HEIGHT - 1}
                stroke="var(--border)"
                strokeWidth="1"
                vectorEffect="non-scaling-stroke"
              />
              <circle cx={activePoint.x} cy={activePoint.y} r="2.25" fill="var(--background)" stroke={stroke} strokeWidth="1.5" />
            </>
          ) : null}
        </svg>
      </span>
    </Tooltip>
  )
}
