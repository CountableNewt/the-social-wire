"use client";

import { createContext } from "react";

/** Slot in the Article Library section bar where list-level controls (e.g. Expand All) render. */
export const ArticleLibraryToolbarContext = createContext<HTMLElement | null>(null);
