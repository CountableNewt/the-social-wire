"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/hooks/useAuth";
import { AppSidebar } from "@/components/AppSidebar/AppSidebar";
import { ReadRouteProvider } from "@/contexts/ReadRouteContext";
import {
  SidebarProvider,
  SidebarInset,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { Separator } from "@/components/ui/separator";
import { normalizeAtRepoParam } from "@/lib/atprotoClient";

function ClosePublicationsSheetOnMobilePubRoute({
  selectedPubId,
}: {
  selectedPubId: string | null;
}) {
  const { isMobile, setOpenMobile } = useSidebar();

  useEffect(() => {
    if (isMobile && selectedPubId) {
      setOpenMobile(false);
    }
  }, [isMobile, selectedPubId, setOpenMobile]);

  return null;
}

export default function ReadLayout({ children }: { children: React.ReactNode }) {
  const { session, isLoading } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  // Derive the selected pubId from the URL path: /read/[pubId]
  const selectedPubId = pathname.startsWith("/read/")
    ? normalizeAtRepoParam(pathname.slice("/read/".length))
    : null;

  useEffect(() => {
    if (!isLoading && !session) {
      router.replace("/login");
    }
  }, [isLoading, session, router]);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  if (!session) {
    // Redirect in progress; render nothing to avoid flash
    return null;
  }

  return (
    <SidebarProvider>
      <ReadRouteProvider>
        <ClosePublicationsSheetOnMobilePubRoute selectedPubId={selectedPubId} />
        <AppSidebar
          selectedPubId={selectedPubId}
          onSelectPub={(pubId) => router.push(`/read/${encodeURIComponent(pubId)}`)}
        />
        <SidebarInset className="flex flex-col min-h-0 flex-1">
          <header className="flex h-10 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="h-4" />
          </header>
          <main className="flex flex-1 overflow-hidden">{children}</main>
        </SidebarInset>
      </ReadRouteProvider>
    </SidebarProvider>
  );
}
