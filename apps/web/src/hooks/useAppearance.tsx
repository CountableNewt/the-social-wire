"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import {
  BOLD_TEXT_STORAGE_KEY,
  FONT_FAMILY_STORAGE_KEY,
  FONT_STORAGE_KEY,
  isBoldTextPreference,
  isFontFamilyPreference,
  isFontPreference,
  isThemePreference,
  resolveComputedTheme,
  THEME_STORAGE_KEY,
  type ComputedTheme,
  type FontFamilyPreference,
  type FontPreference,
  type ThemePreference,
} from "@/lib/appearance";

type AppearanceContextValue = {
  theme: ThemePreference;
  computedTheme: ComputedTheme;
  setTheme: (theme: ThemePreference) => void;
  font: FontPreference;
  setFont: (font: FontPreference) => void;
  fontFamily: FontFamilyPreference;
  setFontFamily: (fontFamily: FontFamilyPreference) => void;
  boldText: boolean;
  setBoldText: (enabled: boolean) => void;
};

const AppearanceContext = createContext<AppearanceContextValue | null>(null);
const subscribers = new Set<() => void>();
const SERVER_SNAPSHOT = "system|sans|system|0|light";

function readStoredTheme(): ThemePreference {
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    return isThemePreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

function readStoredFont(): FontPreference {
  try {
    const stored = window.localStorage.getItem(FONT_STORAGE_KEY);
    return isFontPreference(stored) ? stored : "sans";
  } catch {
    return "sans";
  }
}

function readStoredFontFamily(): FontFamilyPreference {
  try {
    const stored = window.localStorage.getItem(FONT_FAMILY_STORAGE_KEY);
    return isFontFamilyPreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

function readStoredBoldText(): boolean {
  try {
    const stored = window.localStorage.getItem(BOLD_TEXT_STORAGE_KEY);
    return isBoldTextPreference(stored) ? stored === "1" : false;
  } catch {
    return false;
  }
}

function readSystemDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function getSnapshot(): string {
  return [
    readStoredTheme(),
    readStoredFont(),
    readStoredFontFamily(),
    readStoredBoldText() ? "1" : "0",
    readSystemDark() ? "dark" : "light",
  ].join("|");
}

function parseSnapshot(snapshot: string) {
  const [storedTheme, storedFont, storedFontFamily, storedBold, systemTheme] =
    snapshot.split("|");
  return {
    theme: isThemePreference(storedTheme) ? storedTheme : "system",
    font: isFontPreference(storedFont) ? storedFont : "sans",
    fontFamily: isFontFamilyPreference(storedFontFamily)
      ? storedFontFamily
      : "system",
    boldText: isBoldTextPreference(storedBold) ? storedBold === "1" : false,
    systemDark: systemTheme === "dark",
  };
}

function subscribe(listener: () => void): () => void {
  subscribers.add(listener);
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  media.addEventListener?.("change", listener);
  window.addEventListener("storage", listener);

  return () => {
    subscribers.delete(listener);
    media.removeEventListener?.("change", listener);
    window.removeEventListener("storage", listener);
  };
}

function notify() {
  subscribers.forEach((listener) => listener());
}

function applyAppearance(
  theme: ThemePreference,
  systemDark: boolean,
  font: FontPreference,
  fontFamily: FontFamilyPreference,
  boldText: boolean,
) {
  const computedTheme = resolveComputedTheme(theme, systemDark);
  const root = document.documentElement;
  root.classList.toggle("dark", computedTheme === "dark");
  root.classList.toggle("light", computedTheme === "light");
  root.dataset.theme = theme;
  root.dataset.computedTheme = computedTheme;
  root.dataset.font = font;
  root.dataset.fontFamily = fontFamily;
  root.dataset.boldText = boldText ? "true" : "false";
  root.style.colorScheme = computedTheme;
}

export function AppearanceProvider({ children }: { children: ReactNode }) {
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, () => SERVER_SNAPSHOT);
  const { theme, font, fontFamily, boldText, systemDark } =
    parseSnapshot(snapshot);

  useEffect(() => {
    applyAppearance(theme, systemDark, font, fontFamily, boldText);
  }, [boldText, font, fontFamily, systemDark, theme]);

  const setTheme = useCallback((nextTheme: ThemePreference) => {
    try {
      if (nextTheme === "system") {
        window.localStorage.removeItem(THEME_STORAGE_KEY);
      } else {
        window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme);
      }
    } catch {
      // Ignore unavailable browser storage.
    }
    notify();
  }, []);

  const setFont = useCallback((nextFont: FontPreference) => {
    try {
      if (nextFont === "sans") {
        window.localStorage.removeItem(FONT_STORAGE_KEY);
      } else {
        window.localStorage.setItem(FONT_STORAGE_KEY, nextFont);
      }
    } catch {
      // Ignore unavailable browser storage.
    }
    notify();
  }, []);

  const setFontFamily = useCallback((nextFontFamily: FontFamilyPreference) => {
    try {
      if (nextFontFamily === "system") {
        window.localStorage.removeItem(FONT_FAMILY_STORAGE_KEY);
      } else {
        window.localStorage.setItem(FONT_FAMILY_STORAGE_KEY, nextFontFamily);
      }
    } catch {
      // Ignore unavailable browser storage.
    }
    notify();
  }, []);

  const setBoldText = useCallback((enabled: boolean) => {
    try {
      if (enabled) {
        window.localStorage.setItem(BOLD_TEXT_STORAGE_KEY, "1");
      } else {
        window.localStorage.removeItem(BOLD_TEXT_STORAGE_KEY);
      }
    } catch {
      // Ignore unavailable browser storage.
    }
    notify();
  }, []);

  const value = useMemo(
    () => ({
      theme,
      computedTheme: resolveComputedTheme(theme, systemDark),
      setTheme,
      font,
      setFont,
      fontFamily,
      setFontFamily,
      boldText,
      setBoldText,
    }),
    [
      boldText,
      font,
      fontFamily,
      setBoldText,
      setFont,
      setFontFamily,
      setTheme,
      systemDark,
      theme,
    ],
  );

  return (
    <AppearanceContext.Provider value={value}>
      {children}
    </AppearanceContext.Provider>
  );
}

export function useAppearance(): AppearanceContextValue {
  const context = useContext(AppearanceContext);
  if (!context) {
    throw new Error("useAppearance must be used within AppearanceProvider");
  }
  return context;
}
