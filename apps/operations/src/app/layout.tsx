import type { Metadata, Viewport } from "next"
import { Providers } from "./providers"
import "./globals.css"

export const metadata: Metadata = {
  metadataBase: new URL("https://operations.thesocialwire.app"),
  title: "The Social Wire Operations",
  description: "Operations, observability, and recovery control plane",
  robots: { index: false, follow: false },
  openGraph: {
    title: "The Social Wire Operations",
    description: "Operations, observability, and recovery control plane",
    type: "website",
    images: [{ url: "/og/the-social-wire-operations.png", width: 1200, height: 630, alt: "The Social Wire Operations" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "The Social Wire Operations",
    description: "Operations, observability, and recovery control plane",
    images: ["/og/the-social-wire-operations.png"],
  },
}

export const viewport: Viewport = { width: "device-width", initialScale: 1, colorScheme: "light dark" }

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body><Providers>{children}</Providers></body>
    </html>
  )
}
