"use client"
import * as React from "react"
import { beginSignIn, endSession, restoreSession, type OAuthSession } from "@/lib/auth"

type AuthState = { session: OAuthSession | null; loading: boolean; forbidden: boolean; setForbidden: (value: boolean) => void; signIn: (handle: string) => Promise<void>; signOut: () => Promise<void> }
const AuthContext = React.createContext<AuthState | null>(null)

export function OperationsAuthProvider({ children }: { children: React.ReactNode }) {
  const demo = process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1"
  const [session, setSession] = React.useState<OAuthSession | null>(null)
  const [loading, setLoading] = React.useState(!demo)
  const [forbidden, setForbidden] = React.useState(false)
  React.useEffect(() => { if (!demo) void restoreSession().then(setSession).finally(() => setLoading(false)) }, [demo])
  const value = React.useMemo<AuthState>(() => ({
    session, loading, forbidden, setForbidden,
    signIn: beginSignIn,
    signOut: async () => { if (session) await endSession(session.did); setSession(null) },
  }), [session, loading, forbidden])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useOperationsAuth() { const value = React.useContext(AuthContext); if (!value) throw new Error("useOperationsAuth must be used inside OperationsAuthProvider"); return value }
