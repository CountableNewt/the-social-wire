"use client";

import { useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";

export function ProfileImageField({ label, currentUrl, hasImage, value, onChange, disabled }: {
  label: "Avatar" | "Banner";
  currentUrl?: string;
  hasImage: boolean;
  value: File | null | undefined;
  onChange: (value: File | null) => void;
  disabled: boolean;
}) {
  const id = useId();
  const [preview, setPreview] = useState<{ file: File; url: string }>();
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview.url); }, [preview]);
  const url = value === null ? undefined : preview && preview.file === value ? preview.url : currentUrl;
  return <div className="min-w-0 space-y-2">
    <label htmlFor={id} className="block text-sm font-medium">{label}</label>
    {url ? /* eslint-disable-next-line @next/next/no-img-element -- user-selected file or public PDS image */
      <img src={url} alt={`${label} Preview`} className={label === "Avatar" ? "size-16 rounded-full object-cover" : "h-24 w-full rounded-lg object-cover"} referrerPolicy="no-referrer" /> : null}
    <input id={id} type="file" accept="image/jpeg,image/png" disabled={disabled} className="block w-full min-w-0 text-xs file:mr-2 file:rounded file:border file:bg-background file:px-3 file:py-2" onChange={event => {
      const file = event.target.files?.[0];
      if (file) { setPreview({ file, url: URL.createObjectURL(file) }); onChange(file); }
      event.target.value = "";
    }} />
    {(value instanceof File || (value !== null && hasImage)) ? <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => onChange(null)}>Remove {label}</Button> : null}
  </div>;
}
