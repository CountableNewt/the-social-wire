import type { EnvironmentName } from "@/lib/operations-types"

export function operationsEnvironment(): EnvironmentName {
  const value = process.env.NEXT_PUBLIC_APP_ENV?.trim().toLowerCase()
  return value === "prod" || value === "production" ? "production" : "development"
}
