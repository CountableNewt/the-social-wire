type Result = { label: string; requests: number; throughputPerSecond: number; p95Milliseconds: number }

const requests = Number(process.env.BENCHMARK_REQUESTS ?? 500)
const concurrency = Number(process.env.BENCHMARK_CONCURRENCY ?? 10)
const path = process.env.BENCHMARK_PATH ?? "/health"
const baselineOrigin = required("BASELINE_ORIGIN")
const telemetryOrigin = required("TELEMETRY_ORIGIN")

const baseline = await run("baseline", baselineOrigin)
const telemetry = await run("telemetry", telemetryOrigin)
const throughputRegression = (baseline.throughputPerSecond - telemetry.throughputPerSecond) / baseline.throughputPerSecond
const p95Regression = (telemetry.p95Milliseconds - baseline.p95Milliseconds) / baseline.p95Milliseconds

console.log(JSON.stringify({ baseline, telemetry, throughputRegression, p95Regression }, null, 2))
if (throughputRegression > 0.05 || p95Regression > 0.05) {
  throw new Error("Operations telemetry exceeded the 5% throughput or p95 regression budget")
}

async function run(label: string, origin: string): Promise<Result> {
  const durations: number[] = []
  let next = 0
  const startedAt = performance.now()
  await Promise.all(Array.from({ length: concurrency }, async () => {
    while (true) {
      const index = next++
      if (index >= requests) return
      const started = performance.now()
      const response = await fetch(`${origin}${path}`, { headers: requestHeaders() })
      if (!response.ok) throw new Error(`${label} request failed (${response.status})`)
      await response.arrayBuffer()
      durations.push(performance.now() - started)
    }
  }))
  const elapsedSeconds = (performance.now() - startedAt) / 1_000
  durations.sort((left, right) => left - right)
  return {
    label,
    requests,
    throughputPerSecond: requests / elapsedSeconds,
    p95Milliseconds: durations[Math.min(durations.length - 1, Math.ceil(durations.length * 0.95) - 1)] ?? 0,
  }
}

function requestHeaders() {
  const headers = new Headers({ Accept: "application/json" })
  if (process.env.BENCHMARK_AUTHORIZATION) headers.set("Authorization", process.env.BENCHMARK_AUTHORIZATION)
  if (process.env.BENCHMARK_DPOP) headers.set("DPoP", process.env.BENCHMARK_DPOP)
  return headers
}

function required(name: string) {
  const value = process.env[name]?.replace(/\/$/, "")
  if (!value) throw new Error(`${name} is required`)
  return value
}
