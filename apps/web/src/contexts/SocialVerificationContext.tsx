"use client";

import { createContext, useContext, useLayoutEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from "react";
import { useQueries } from "@tanstack/react-query";
import { useAuth } from "@/hooks/useAuth";
import { DEFAULT_VERIFICATION_PREFERENCES, VERIFICATION_STORAGE_PREFIX, loadCustomVerifications, parseVerificationPreferences, type CustomVerification, type VerificationPreferences } from "@/lib/socialVerification";

const CHANGE_EVENT = "social-wire-verification-preferences";
function subscribe(callback: () => void) {
  window.addEventListener("storage", callback);
  window.addEventListener(CHANGE_EVENT, callback);
  return () => { window.removeEventListener("storage", callback); window.removeEventListener(CHANGE_EVENT, callback); };
}
const SocialVerificationContext = createContext({ preferences: DEFAULT_VERIFICATION_PREFERENCES, records: [] as CustomVerification[], loading: false, error: undefined as string | undefined, available: false, update: (_value: VerificationPreferences | ((previous: VerificationPreferences) => VerificationPreferences)): void => { void _value; throw new Error("Log In to Change Trusted Verifiers."); }, retry: () => {} });
export function SocialVerificationProvider({ children }: { children: ReactNode }) {
  const { session } = useAuth(), did = session?.did;
  const currentDid = useRef(did);
  useLayoutEffect(() => { currentDid.current = did; }, [did]);
  const raw = useSyncExternalStore(subscribe, () => {
    try { return did ? window.localStorage.getItem(VERIFICATION_STORAGE_PREFIX + did) : null; } catch { return null; }
  }, () => null);
  const preferences = useMemo(() => parseVerificationPreferences(raw), [raw]);
  const queries = useQueries({ queries: preferences.trustedVerifierDIDs.map(issuer => ({ queryKey: ["customVerifications", did, issuer], queryFn: ({ signal }: { signal: AbortSignal }) => loadCustomVerifications(issuer, signal), enabled: !!did, staleTime: 5 * 60_000, retry: false })) });
  const records = queries.flatMap(query => query.isError ? [] : query.data ?? []);
  const error = queries.find(query => query.error)?.error;
  return <SocialVerificationContext.Provider value={{ preferences, records, available: !!did, loading: queries.some(query => query.isPending), error: error instanceof Error ? error.message : undefined, update: value => {
    if (!did || currentDid.current !== did) throw new Error("Your Account Changed. Reopen Settings.");
    const previous = parseVerificationPreferences(window.localStorage.getItem(VERIFICATION_STORAGE_PREFIX + did));
    const next = typeof value === "function" ? value(previous) : value;
    window.localStorage.setItem(VERIFICATION_STORAGE_PREFIX + did, JSON.stringify(next));
    window.dispatchEvent(new window.Event(CHANGE_EVENT));
  }, retry: () => { queries.forEach(query => { void query.refetch(); }); } }}>{children}</SocialVerificationContext.Provider>;
}
export function useSocialVerification() { return useContext(SocialVerificationContext); }
