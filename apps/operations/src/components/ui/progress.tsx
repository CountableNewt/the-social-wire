import { cn } from "@/lib/utils"
export function Progress({
  value,
  className,
  ariaValueText,
}: {
  value: number
  className?: string
  ariaValueText?: string
}) {
  const boundedValue = Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : 0
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={boundedValue}
      aria-valuetext={ariaValueText}
      className={cn("h-1.5 overflow-hidden rounded-full bg-muted", className)}
    >
      <div className="h-full bg-primary transition-[width]" style={{ width: `${boundedValue}%` }} />
    </div>
  )
}
