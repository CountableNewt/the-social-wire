import type { NextConfig } from "next"
import path from "node:path"

const nextConfig: NextConfig = {
  turbopack: { root: path.resolve(__dirname, "../..") },
  outputFileTracingRoot: path.resolve(__dirname, "../.."),
  outputFileTracingIncludes: {
    "/**": ["../../docs/runbooks/operations/*.md"],
  },
}

export default nextConfig
