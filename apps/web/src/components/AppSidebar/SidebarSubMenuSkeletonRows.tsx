"use client";

import { Skeleton } from "@/components/ui/skeleton";
import { SidebarMenuSubItem } from "@/components/ui/sidebar";

export function SidebarSubMenuSkeletonRows({ count = 2 }: { count?: number }) {
  return (
    <>
      {Array.from({ length: count }).map((_, index) => (
        <SidebarMenuSubItem key={index}>
          <Skeleton className="mx-2 h-7 w-[calc(100%-1rem)]" />
        </SidebarMenuSubItem>
      ))}
    </>
  );
}
