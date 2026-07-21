"use client"

import { useQuery } from "@tanstack/react-query"
import { Activity } from "lucide-react"
import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { BackfillSheet } from "@/components/operations/backfills/backfill-sheet"
import { GapInvestigationSheet } from "@/components/operations/gaps/gap-investigation-sheet"
import { OperationsLoadingScreen } from "@/components/operations/shell/operations-loading-screen"
import { MobileOperationsNav } from "@/components/operations/shell/operations-mobile-nav"
import { operationsNav } from "@/components/operations/shell/operations-navigation"
import { OperationsPageHeading } from "@/components/operations/shell/operations-page-heading"
import { OperationsRouteContent } from "@/components/operations/shell/operations-route-content"
import { OperationsTopBar } from "@/components/operations/shell/operations-top-bar"
import type { Runbook } from "@/components/operations/shell/operations-view-types"
import { OverviewSkeleton } from "@/components/operations/dashboard/overview-skeleton"
import { OperatorSignIn } from "@/components/operations/sign-in"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarNavButton,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { operationsEnvironment } from "@/lib/app-environment"
import { useOperationsAuth } from "@/lib/auth-context"
import { fetchOverview, OperationsForbiddenError } from "@/lib/operations-api"
import type { Gap } from "@/lib/operations-types"

export function OperationsConsole({ section, runbooks }: { section: string[]; runbooks: Runbook[] }) {
  const router = useRouter()
  const auth = useOperationsAuth()
  const demo = process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1"
  const environment = operationsEnvironment()
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [demoSessionActive, setDemoSessionActive] = useState(true)
  const [selectedGap, setSelectedGap] = useState<Gap>()
  const [investigationGap, setInvestigationGap] = useState<Gap>()
  const [drawerDismissed, setDrawerDismissed] = useState(false)
  const current = section[0] || "overview"
  const overview = useQuery({
    queryKey: ["operations-overview", environment],
    queryFn: () => fetchOverview(auth.session),
    enabled: demo || Boolean(auth.session),
    refetchInterval: autoRefresh ? 15_000 : false,
    placeholderData: (previous) => previous,
  })

  useEffect(() => {
    if (overview.error instanceof OperationsForbiddenError) auth.setForbidden(true)
  }, [overview.error, auth])

  if (auth.loading) return <OperationsLoadingScreen />
  if (!auth.session && (!demo || !demoSessionActive)) return <OperatorSignIn />

  const data = overview.data
  const effectiveSelectedGap = selectedGap ?? (demo && !drawerDismissed ? data?.gaps[0] : undefined)

  return (
    <SidebarProvider>
      <Sidebar>
        <SidebarHeader>
          <div className="flex min-w-0 items-center gap-2">
            <span className="grid size-7 shrink-0 place-items-center text-primary">
              <Activity className="size-5" />
            </span>
            <div className="min-w-0">
              <p className="truncate text-xs font-semibold">The Social Wire</p>
              <p className="ops-label">Operations</p>
            </div>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <nav aria-label="Operations">
            <div className="grid gap-1">
              {operationsNav.map(([key, label, Icon]) => (
                <SidebarNavButton
                  key={key}
                  active={current === key}
                  icon={<Icon className="size-3.5" />}
                  onClick={() => router.push(key === "overview" ? "/" : `/${key}`)}
                >
                  {label}
                </SidebarNavButton>
              ))}
            </div>
          </nav>
        </SidebarContent>
        <SidebarFooter>
          <SidebarTrigger />
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <OperationsTopBar
          environment={environment}
          autoRefresh={autoRefresh}
          setAutoRefresh={setAutoRefresh}
          refreshedAt={data?.refreshedAt}
          onRefresh={() => overview.refetch()}
          operator={auth.session?.did ?? "did:plc:demo-operator"}
          onSignOut={async () => {
            await auth.signOut()
            if (demo) setDemoSessionActive(false)
          }}
        />
        <MobileOperationsNav current={current} />
        <div className="p-3 sm:p-4">
          <OperationsPageHeading current={current} />
          {overview.isLoading || !data ? (
            <OverviewSkeleton />
          ) : (
            <OperationsRouteContent
              current={current}
              traceId={section[1]}
              data={data}
              environment={environment}
              runbooks={runbooks}
              onSelectGap={(gap) => {
                setSelectedGap(gap)
                setDrawerDismissed(false)
              }}
              onInvestigateGap={setInvestigationGap}
            />
          )}
          {overview.error && !(overview.error instanceof OperationsForbiddenError) ? (
            <p role="alert" className="mt-3 text-xs text-destructive">
              {overview.error.message}
            </p>
          ) : null}
        </div>
      </SidebarInset>
      <BackfillSheet
        key={`${environment}-${effectiveSelectedGap?.id ?? "none"}`}
        gap={effectiveSelectedGap}
        environment={environment}
        open={Boolean(effectiveSelectedGap)}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedGap(undefined)
            setDrawerDismissed(true)
          }
        }}
      />
      <GapInvestigationSheet
        gap={investigationGap}
        open={Boolean(investigationGap)}
        onOpenChange={(open) => {
          if (!open) setInvestigationGap(undefined)
        }}
        onBackfill={(gap) => {
          setInvestigationGap(undefined)
          setSelectedGap(gap)
          setDrawerDismissed(false)
        }}
      />
    </SidebarProvider>
  )
}
