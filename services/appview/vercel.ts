import { appViewVercelConfig } from "./scripts/vercel-config";

export const config = appViewVercelConfig(process.env.APP_ENV, process.env.VERCEL_ENV, process.env.VERCEL_GIT_COMMIT_REF);
