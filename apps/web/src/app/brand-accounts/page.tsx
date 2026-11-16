import { Suspense } from "react";
import { BrandAccountsWorkspace } from "@/components/BrandAccounts/BrandAccountsWorkspace";
import { notFound } from "next/navigation";
import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";

export default function BrandAccountsPage() {
  if (!brandAccountsEnabled()) notFound();
  return <Suspense fallback={<p role="status">Loading Delegated Accounts…</p>}><BrandAccountsWorkspace /></Suspense>;
}
