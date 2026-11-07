/** Build-time opt-in. Scheduling remains unavailable unless explicitly enabled. */
export function isPostSchedulingEnabled(): boolean {
  return process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING === "true";
}

export function assertPostSchedulingEnabled(): void {
  if (!isPostSchedulingEnabled()) throw new Error("Post Scheduling Is Not Enabled.");
}
