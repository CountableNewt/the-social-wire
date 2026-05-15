import type { Metadata } from "next";
import type { CSSProperties } from "react";
import { Analytics } from "@vercel/analytics/react";
import "./globals.css";
import { Providers } from "./providers";
import { EnvironmentBanner } from "@/components/shared/EnvironmentBanner";

export const metadata: Metadata = {
  title: "The Social Wire",
  description: "A reader for the standard.site publishing ecosystem",
};

const env = process.env.NEXT_PUBLIC_APP_ENV ?? "local";
const environmentBannerHeight = env === "prod" ? "0px" : "32px";

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="h-full antialiased" suppressHydrationWarning>
      <head>
        <script
          id="dark-mode"
          dangerouslySetInnerHTML={{
            __html: `(function(){try{if(window.matchMedia('(prefers-color-scheme: dark)').matches)document.documentElement.classList.add('dark');}catch(e){}})();`,
          }}
        />
      </head>
      <body
        className="min-h-full flex flex-col"
        style={
          {
            "--environment-banner-height": environmentBannerHeight,
          } as CSSProperties
        }
      >
        <Providers>
          <EnvironmentBanner />
          {children}
        </Providers>
        <Analytics />
      </body>
    </html>
  );
}
