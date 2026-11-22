export function getSkejEnvironment() {
  const api = new URL(process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL ?? "https://api.thesocialwire.app");
  return { api: api.origin, connectUrl: new URL("/oauth/scheduling/start", api).href };
}

export const SKEJ_SCHEDULING_METHODS = ["at.skej.actor.getSession", "at.skej.schedule.list", "at.skej.schedule.create", "at.skej.schedule.update", "at.skej.schedule.cancel"] as const;
