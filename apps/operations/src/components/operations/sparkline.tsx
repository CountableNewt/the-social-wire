"use client"
import { Line, LineChart } from "recharts"
import { ChartContainer } from "@/components/ui/chart"

const values = [5, 7, 4, 9, 6, 10, 8, 12, 7, 9, 6, 11, 8].map((value, index) => ({ index, value }))
export function Sparkline({ tone = "primary" }: { tone?: "primary" | "warning" }) {
  return (
    <ChartContainer className="inline-block h-5 w-16 align-middle">
      <LineChart data={values}>
        <Line
          type="monotone"
          dataKey="value"
          stroke={tone === "warning" ? "var(--warning)" : "var(--primary)"}
          strokeWidth={1.4}
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ChartContainer>
  )
}
