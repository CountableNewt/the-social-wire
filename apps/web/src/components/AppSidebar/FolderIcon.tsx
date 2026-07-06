"use client";

import {
  Bell,
  BookOpen,
  Bot,
  Briefcase,
  Calendar,
  ChartColumn,
  Code,
  Cpu,
  Database,
  Folder,
  Globe,
  Heart,
  Landmark,
  Lightbulb,
  Megaphone,
  Monitor,
  Newspaper,
  Palette,
  Radio,
  Rocket,
  Rss,
  Scale,
  Search,
  Server,
  Shield,
  Sparkles,
  Tag,
  Users,
  Wrench,
  type LucideIcon,
} from "lucide-react";

export type FolderIconOption = {
  value: string;
  label: string;
  Icon: LucideIcon;
};

export const folderIconOptions: FolderIconOption[] = [
  { value: "folder", label: "Folder", Icon: Folder },
  { value: "briefcase", label: "Industry", Icon: Briefcase },
  { value: "palette", label: "Product", Icon: Palette },
  { value: "code", label: "Engineering", Icon: Code },
  { value: "scale", label: "Policy", Icon: Scale },
  { value: "newspaper", label: "News", Icon: Newspaper },
  { value: "rss", label: "Feeds", Icon: Rss },
  { value: "landmark", label: "Civic", Icon: Landmark },
  { value: "radio", label: "Signals", Icon: Radio },
  { value: "users", label: "Audience", Icon: Users },
  { value: "megaphone", label: "Announcements", Icon: Megaphone },
  { value: "book-open", label: "Research", Icon: BookOpen },
  { value: "chart-column", label: "Analytics", Icon: ChartColumn },
  { value: "calendar", label: "Calendar", Icon: Calendar },
  { value: "rocket", label: "Launch", Icon: Rocket },
  { value: "lightbulb", label: "Ideas", Icon: Lightbulb },
  { value: "sparkles", label: "Highlights", Icon: Sparkles },
  { value: "globe", label: "World", Icon: Globe },
  { value: "tag", label: "Tags", Icon: Tag },
  { value: "search", label: "Search", Icon: Search },
  { value: "bell", label: "Alerts", Icon: Bell },
  { value: "monitor", label: "Interface", Icon: Monitor },
  { value: "cpu", label: "Platform", Icon: Cpu },
  { value: "database", label: "Data", Icon: Database },
  { value: "server", label: "Infrastructure", Icon: Server },
  { value: "shield", label: "Security", Icon: Shield },
  { value: "wrench", label: "Tools", Icon: Wrench },
  { value: "bot", label: "Automation", Icon: Bot },
  { value: "heart", label: "Favorites", Icon: Heart },
];

const folderIconMap: Record<string, LucideIcon> = Object.fromEntries(
  folderIconOptions.map((option) => [option.value, option.Icon])
);

folderIconMap.government = Landmark;
folderIconMap.policy = Scale;

export function FolderIconGlyph({
  icon,
  iconImage,
  name,
  className = "h-4 w-4",
}: {
  icon?: string;
  iconImage?: string;
  name: string;
  className?: string;
}) {
  if (iconImage) {
    return (
      <>
        {/* eslint-disable-next-line @next/next/no-img-element -- arbitrary folder icon URLs */}
        <img
          src={iconImage}
          alt={name}
          className={`${className} rounded object-cover`}
        />
      </>
    );
  }

  if (icon) {
    const trimmed = icon.trim();
    const Icon = folderIconMap[trimmed.toLowerCase()];
    if (Icon) {
      return <Icon className={`${className} shrink-0`} aria-hidden />;
    }
    if (!/^[a-z0-9_-]+$/i.test(trimmed)) {
      return (
        <span className="text-sm leading-none" aria-hidden>
          {trimmed}
        </span>
      );
    }
  }

  return <Folder className={`${className} shrink-0`} aria-hidden />;
}
