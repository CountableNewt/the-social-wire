import path from "path";

import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  allowedDevOrigins: ["127.0.0.1", "[::1]"],
  /**
   * Monorepo root so Turbopack resolves `next` from the workspace (setting this to only `apps/web`
   * breaks `next build` with package resolution errors). Heavy dev-mode churn from Turbopack is
   * avoided by running `next dev --webpack` in the package `dev` script.
   */
  turbopack: {
    root: path.join(__dirname, "..", ".."),
  },
  async headers() {
    return [
      {
        source: "/client-metadata.json",
        headers: [{ key: "Access-Control-Allow-Origin", value: "*" }],
      },
    ];
  },
};

export default nextConfig;
