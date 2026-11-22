import { socialWireVercelConfig } from "./scripts/vercel-config";

export const config = socialWireVercelConfig(process.env.APP_ENV, process.env.VERCEL_ENV, process.env.VERCEL_GIT_COMMIT_REF);
