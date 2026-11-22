import { getAppEnv } from "@/lib/appEnv";

export function livestreamEnabled(): boolean {
  const configured = process.env.NEXT_PUBLIC_LIVESTREAM_ENABLED;
  if (configured !== undefined) return configured === "true";
  return getAppEnv() !== "prod";
}
