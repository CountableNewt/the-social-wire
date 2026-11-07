"use client";

import { createContext, type RefObject } from "react";

export interface SocialFeedViewport {
  container: RefObject<HTMLDivElement | null>;
  scope: string;
  resetKey: number;
}

export const SocialFeedViewportContext = createContext<SocialFeedViewport | null>(null);
