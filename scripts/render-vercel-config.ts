import { socialWireVercelConfig } from "./vercel-config";

// Render into a private deployment archive, never over the source entrypoint.
// Native CLI --local-config accepts JSON, not the TypeScript config entrypoint.
process.stdout.write(`${JSON.stringify(socialWireVercelConfig(
  process.env.APP_ENV,
  process.env.VERCEL_ENV,
  process.env.VERCEL_GIT_COMMIT_REF,
), null, 2)}\n`);
