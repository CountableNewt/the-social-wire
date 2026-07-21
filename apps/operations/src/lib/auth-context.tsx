"use client"
import * as React from "react"
import { beginSignIn, endSession, restoreSession, type OAuthSession } from "@/lib/auth"
import { isConfiguredOperatorDid } from "@/lib/operator-access"

type AuthState = {
  session: OAuthSession | null
  loading: boolean
  forbidden: boolean
  setForbidden: (value: boolean) => void
  signIn: (handle: string) => Promise<void>
  signOut: () => Promise<void>
}
const AuthContext = React.createContext<AuthState | null>(null)

export function OperationsAuthProvider({ children }: { children: React.ReactNode }) {
  const demo = process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1"
  const [session, setSession] = React.useState<OAuthSession | null>(null)
  const [loading, setLoading] = React.useState(!demo)
  const [forbidden, setForbiddenState] = React.useState(false)
  React.useEffect(() => {
    if (demo) return
    void restoreSession()
      .then(async (restored) => {
        if (!restored) return
        if (isConfiguredOperatorDid(restored.did)) {
          setSession(restored)
          return
        }
        setForbiddenState(true)
        await endSession(restored.did).catch(() => undefined)
      })
      .finally(() => setLoading(false))
  }, [demo])
  const setForbidden = React.useCallback(
    (value: boolean) => {
      setForbiddenState(value)
      if (value && session) {
        setSession(null)
        void endSession(session.did).catch(() => undefined)
      }
    },
    [session],
  )
  const value = React.useMemo<AuthState>(
    () => ({
      session,
      loading,
      forbidden,
      setForbidden,
      signIn: beginSignIn,
      signOut: async () => {
        try {
          if (session) await endSession(session.did)
        } finally {
          setSession(null)
          setForbiddenState(false)
        }
      },
    }),
    [session, loading, forbidden, setForbidden],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useOperationsAuth() {
  const value = React.useContext(AuthContext)
  if (!value) throw new Error("useOperationsAuth must be used inside OperationsAuthProvider")
  return value
}
