import type { NextConfig } from "next"
import path from "node:path"

const nextConfig: NextConfig = {
  env: {
    NEXT_PUBLIC_APP_ENV: process.env.NEXT_PUBLIC_APP_ENV ?? process.env.APP_ENV ?? "",
    NEXT_PUBLIC_OPERATIONS_OPERATOR_DIDS:
      process.env.NEXT_PUBLIC_OPERATIONS_OPERATOR_DIDS ?? process.env.OPERATIONS_OPERATOR_DIDS ?? "",
  },
  turbopack: { root: path.resolve(__dirname, "../..") },
  outputFileTracingRoot: path.resolve(__dirname, "../.."),
  outputFileTracingIncludes: {
    "/**": ["../../docs/runbooks/operations/*.md"],
  },
}

export default nextConfig
