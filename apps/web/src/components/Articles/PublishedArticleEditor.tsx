"use client";

import { useRef, useState } from "react";
import type { BlockEditorHandle } from "@stygian/markdown-editor";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { publishedArticleMarkdown, type ArticleEditCapability } from "@/lib/articles/articleEditCapability";
import type { PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { ArticleBodyViews, type ArticleBodyMode } from "./ArticleBodyViews";

export type PublishedArticleEdit = {
  title: string;
  description: string;
  path: string;
  tags: string[];
  /** Absent when the article keeps its native body. */
  markdown?: string;
};

const resolveImageURL = (url: string) => /^https:\/\//.test(url) ? url : undefined;

export function PublishedArticleEditor({ article, capability, busy, onCancel, onSave }: {
  article: PublishedArticle;
  capability: Exclude<ArticleEditCapability, "none">;
  busy: boolean;
  onCancel: () => void;
  onSave: (edit: PublishedArticleEdit) => void;
}) {
  const { record } = article;
  const [title, setTitle] = useState(typeof record.title === "string" ? record.title : "");
  const [description, setDescription] = useState(typeof record.description === "string" ? record.description : "");
  const [path, setPath] = useState(typeof record.path === "string" ? record.path : "");
  const [tags, setTags] = useState(Array.isArray(record.tags) ? record.tags.filter(tag => typeof tag === "string").join(", ") : "");
  const [markdown, setMarkdown] = useState(capability === "full" ? publishedArticleMarkdown(record) ?? "" : undefined);
  const [mode, setMode] = useState<ArticleBodyMode>("editor");
  const editor = useRef<BlockEditorHandle>(null);
  const canSave = !busy && !!title.trim() && (markdown === undefined || !!markdown.trim());
  const save = () => onSave({ title, description, path, tags: tags.split(",").map(tag => tag.trim()).filter(Boolean), ...(markdown === undefined ? {} : { markdown }) });
  return <section aria-label="Edit Published Article" aria-busy={busy} className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4 sm:p-6">
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <Button size="sm" disabled={!canSave} onClick={save}>{busy ? "Saving…" : "Save Changes"}</Button>
      <Button size="sm" variant="outline" disabled={busy} onClick={onCancel}>Cancel</Button>
    </div>
    {capability === "details" ? <p className="mb-4 rounded-lg bg-muted p-3 text-sm">This Publication Stores Articles in Its Own Format. Edit the Details Here and the Body in Its Publishing App.</p> : null}
    <div className="mb-5 grid gap-4">
      <label className="block space-y-1 text-sm"><span className="font-medium">Title</span><Input aria-label="Article Title" placeholder="Untitled Article" value={title} disabled={busy} onChange={event => setTitle(event.target.value)} className="h-auto py-2 text-xl font-semibold" /></label>
      <label className="block space-y-1 text-sm"><span>Excerpt</span><textarea aria-label="Article Excerpt" value={description} disabled={busy} onChange={event => setDescription(event.target.value)} rows={3} className="w-full rounded-md border bg-background p-2 text-sm" /></label>
      <label className="block space-y-1 text-sm"><span>Path</span><Input aria-label="Article Path" placeholder="/my-article" value={path} disabled={busy} onChange={event => setPath(event.target.value)} /></label>
      <label className="block space-y-1 text-sm"><span>Tags</span><Input aria-label="Article Tags" placeholder="Comma-Separated Tags" value={tags} disabled={busy} onChange={event => setTags(event.target.value)} /></label>
    </div>
    {markdown !== undefined ? <ArticleBodyViews draftId={article.uri} markdown={markdown} mode={mode} onModeChange={setMode} onChange={setMarkdown} editorRef={editor} resolveImageURL={resolveImageURL} onImageFiles={async () => {}} readOnly={busy} busy={busy} /> : null}
  </section>;
}
