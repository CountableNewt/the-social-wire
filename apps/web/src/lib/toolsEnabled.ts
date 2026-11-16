import { getAppEnv, isNonProd } from "@/lib/appEnv";

/** Deployment availability is independent of the viewer's Pro entitlement. */
export function toolsEnabled(): boolean {
  const flag = process.env.NEXT_PUBLIC_TOOLS_ENABLED;
  if (flag !== undefined) return flag === "true";
  return isNonProd(getAppEnv());
}

export function mediaEnabled(): boolean {
  return process.env.NEXT_PUBLIC_MEDIA_ENABLED !== "false";
}

export function livestreamEnabled(): boolean {
  return process.env.NEXT_PUBLIC_LIVESTREAM_ENABLED !== "false";
}
