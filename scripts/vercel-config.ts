// Production and Development share the team's Hobby schedule allowance. Keep
// one dispatcher per release environment. Only the explicitly configured dev
// Preview branch may declare Development maintenance; other previews stay idle.
export const appViewSurfaces = ["reader", "publishing", "scheduling", "analytics", "integrations", "maintenance"] as const;
export type AppViewSurface = typeof appViewSurfaces[number];

// Preserve public paths while assigning each namespace to its owning Function.
// Specific routes must precede reader namespaces and the final Web catch-all.
export const appViewRouteOwnership: { surface: AppViewSurface; prefix?: string; path?: string }[] = [
  { surface: "maintenance", prefix: "/internal/jobs/" },
  { surface: "maintenance", prefix: "/internal/cron/" },
  { surface: "maintenance", prefix: "/internal/schedules/" },
  { surface: "publishing", prefix: "/xrpc/app.thesocialwire.articleDraft." },
  { surface: "publishing", prefix: "/xrpc/app.thesocialwire.socialDraft." },
  { surface: "publishing", path: "/v1/media/gifs" },
  { surface: "publishing", path: "/v1/media/gifs/thumbnail" },
  { surface: "publishing", path: "/v1/publications/article-embed" },
  { surface: "publishing", path: "/xrpc/app.thesocialwire.publication.getArticleEmbed" },
  { surface: "scheduling", prefix: "/oauth/scheduling/" },
  { surface: "scheduling", prefix: "/xrpc/at.skej." },
  { surface: "analytics", prefix: "/v1/analytics/" },
  { surface: "integrations", prefix: "/xrpc/link.latr." },
  { surface: "integrations", prefix: "/v1/semble/" },
];

export function appViewSurfaceForPath(path: string): AppViewSurface {
  return appViewRouteOwnership.find(rule => rule.path === path || (rule.prefix && path.startsWith(rule.prefix)))?.surface ?? "reader";
}

export function socialWireVercelConfig(appEnv?: string, vercelEnv?: string, gitBranch?: string) {
  const developmentPreview = vercelEnv === "preview" && appEnv === "dev" && gitBranch === "dev";
  const schedules = (vercelEnv === "production" && appEnv === "prod" && gitBranch === "main") || developmentPreview
    ? {
        maintenance: {
          schedule: appEnv === "prod" ? "*/15 * * * *" : "0 * * * *",
          path: "/internal/schedules/maintenance",
          requestMethod: "GET",
          timezone: "UTC",
          jitter: 15,
        },
      }
    : {};

  return {
    "$schema": "https://openapi.vercel.sh/vercel.json",
    git: { deploymentEnabled: { main: true, dev: true, "*": false, "**": false } },
    regions: ["sfo1"],
    env: {
      RUNTIME_MODE: "serverless",
      DATABASE_BACKEND: "turso",
      JOBS_RUNNER_MODE: "external",
    },
    services: {
      web: {
        root: "apps/web",
        framework: "nextjs",
        installCommand: "cd ../.. && bun install --frozen-lockfile",
        buildCommand: "bun run build",
      },
      ...Object.fromEntries(appViewSurfaces.map(surface => [
        surface,
        {
          root: "services/appview",
          framework: "go",
          entrypoint: `cmd/${surface}/main.go`,
          buildCommand: `bash scripts/vercel-build.sh ${surface}`,
          functions: {
            [`cmd/${surface}/main.go`]: { maxDuration: 300, regions: ["sfo1"] },
          },
        },
      ])),
    },
    rewrites: [
      ...appViewRouteOwnership.map(rule => ({
        source: rule.path ?? `${rule.prefix}(.*)`,
        destination: { service: rule.surface },
      })),
      ...["/v1/", "/xrpc/", "/.well-known/"].map(prefix => ({ source: `${prefix}(.*)`, destination: { service: "reader" } })),
      ...["/health", "/healthz", "/livez", "/ready", "/readyz", "/freshness"].map(path => ({ source: path, destination: { service: "reader" } })),
      ...["api.thesocialwire.app", "api.testing.thesocialwire.app"].map(host => ({
        source: "/(.*)", has: [{ type: "host", value: host }], destination: { service: "reader" },
      })),
      { source: "/(.*)", destination: { service: "web" } },
    ],
    schedules,
  };
}
