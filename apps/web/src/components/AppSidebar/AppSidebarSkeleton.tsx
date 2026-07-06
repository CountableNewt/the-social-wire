"use client";

import { Skeleton } from "@/components/ui/skeleton";
import { SidebarMenuItem } from "@/components/ui/sidebar";

export function AppSidebarSkeleton({ count }: { count: number }) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <SidebarMenuItem key={i}>
          <Skeleton className="h-7 w-full rounded-lg" />
        </SidebarMenuItem>
      ))}
    </>
  );
}
