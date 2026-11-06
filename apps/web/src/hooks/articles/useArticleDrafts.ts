"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticleDraftActor } from "@/lib/articles/articleDraftSync";
import { loadSyncedArticleDraft, removeSyncedArticleDraft, syncArticleDraft, syncArticleDrafts } from "@/lib/articles/articleDraftSync";
import { deleteArticleDraft, getArticleDraft, listArticleDrafts, saveArticleDraft, updateArticleDraftSyncState } from "@/lib/articles/articleDraftStorage";

type PersistOptions = { requireRemote?: boolean; localOnly?: boolean };
const locked = (draft: ArticleDraft) => !!(draft.publishedUri || draft.scheduleRecordKey || draft.publishingRecordKey);

/** The actor is injected by the authenticated workspace; storage-only consumers can omit it. */
export function useArticleDrafts(did: string, getSyncActor?: () => ArticleDraftActor) {
  const [drafts, setDrafts] = useState<ArticleDraft[]>([]);
  const [draft, setDraft] = useState<ArticleDraft>();
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const [status, setStatus] = useState("Loading Private Drafts…");
  const [error, setError] = useState<string>();
  const [boundDid] = useState(did);
  const activeDid = useRef(did);
  const actorFactory = useRef(getSyncActor);
  useLayoutEffect(() => { activeDid.current = did; actorFactory.current = getSyncActor; }, [did, getSyncActor]);
  const current = useRef<ArticleDraft | undefined>(undefined);
  const pending = useRef(Promise.resolve());
  const versions = useRef(new Map<string, number>());
  const dirty = useRef(false);
  const editVersion = useRef(0);
  const intent = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const mounted = useRef(true);
  const initialized = useRef(false);
  const lifetime = useRef(new AbortController());
  const networkCount = useRef(0);
  const available = useCallback(() => mounted.current && activeDid.current === boundDid, [boundDid]);
  const requireViewer = useCallback(() => {
    if (did !== boundDid) throw new Error("Your account changed. Reopen Articles before editing private drafts.");
  }, [did, boundDid]);
  const actor = useCallback((): ArticleDraftActor | undefined => {
    const value = actorFactory.current?.();
    if (!value) return;
    return { ...value, signal: value.signal ? AbortSignal.any([value.signal, lifetime.current.signal]) : lifetime.current.signal, assertCurrentActor: () => {
      if (!available() || value.viewerDid !== boundDid) throw new Error("Your Account Changed. Reopen Articles.");
      value.assertCurrentActor?.();
    } };
  }, [available, boundDid]);
  const reportFailure = useCallback((failure: unknown, localSaved = false) => {
    if (!available()) return;
    setStatus(localSaved ? "Saved Locally · Sync Pending" : "Not Saved");
    setError(failure instanceof Error ? failure.message : "The draft could not be saved. Your edits remain open.");
  }, [available]);
  const install = useCallback(async (saved: ArticleDraft | undefined, id: string, version: number, assertCurrentActor?: () => void) => {
    assertCurrentActor?.();
    if (!available()) return;
    if (saved) {
      const knownRevision = versions.current.get(saved.id) ?? 0;
      if ((saved.revision ?? 0) < knownRevision) return;
      versions.current.set(saved.id, Math.max(knownRevision, saved.revision ?? 0));
    }
    if (current.current?.id !== id) return;
    if (!saved) {
      if (dirty.current) {
        const recovery = structuredClone(current.current);
        await updateArticleDraftSyncState(boundDid, id, state => {
          if (!state) throw new Error("Private Draft Sync State Is Unavailable.");
          return { ...state, recoveryDraft: recovery };
        });
        assertCurrentActor?.();
        if (!available() || current.current?.id !== id) return;
      }
      current.current = undefined; dirty.current = false; setDraft(undefined); return;
    }
    if (version === editVersion.current || locked(saved) || saved.updatedAt > current.current.updatedAt) {
      if (dirty.current && version !== editVersion.current && locked(saved)) {
        const recovery = structuredClone(current.current);
        await updateArticleDraftSyncState(boundDid, id, state => {
          if (!state) throw new Error("Private Draft Sync State Is Unavailable.");
          return { ...state, recoveryDraft: recovery };
        });
        assertCurrentActor?.();
        if (!available() || current.current?.id !== id || (saved.revision ?? 0) < (versions.current.get(id) ?? 0)) return;
      }
      current.current = saved; dirty.current = false;
    } else current.current = { ...current.current, revision: Math.max(current.current.revision ?? 0, saved.revision ?? 0) };
    setDraft(current.current);
  }, [available, boundDid]);
  const syncOne = useCallback(async (id: string, version: number, requireRemote = false) => {
    let context: ArticleDraftActor | undefined;
    try {
      context = actor();
      if (!context) {
        if (requireRemote) throw new Error("Private Server Sync Is Required Before Publishing.");
        return;
      }
      networkCount.current++; if (available()) { setSyncing(true); setStatus("Saved Locally · Syncing…"); }
      const saved = await syncArticleDraft(context, id);
      context.assertCurrentActor?.();
      await install(saved, id, version, context.assertCurrentActor);
      if (available()) {
        const rows = await listArticleDrafts(boundDid);
        context.assertCurrentActor?.();
        if (!available()) return;
        setDrafts(rows);
        if (current.current?.id === id || !current.current) {
          setStatus(dirty.current ? "Unsaved Changes" : "Synced Privately"); setError(undefined);
        }
      }
    } catch (failure) {
      let currentActor = true;
      try { context?.assertCurrentActor?.(); } catch { currentActor = false; }
      if (currentActor && available()) reportFailure(failure, true);
      if (requireRemote) throw failure;
    } finally {
      if (context) { networkCount.current = Math.max(0, networkCount.current - 1); if (available()) setSyncing(networkCount.current > 0); }
    }
  }, [actor, available, boundDid, install, reportFailure]);

  const persist = useCallback(async (options: PersistOptions = {}) => {
    requireViewer();
    if (timer.current) clearTimeout(timer.current);
    const snapshot = current.current;
    const snapshotVersion = editVersion.current;
    let write = pending.current;
    if (snapshot && dirty.current) {
      dirty.current = false;
      if (available()) setStatus("Saving…");
      // Local writes never queue behind the network: newer typing reaches the offline journal.
      write = pending.current.catch(() => undefined).then(async () => {
        const saved = await saveArticleDraft(did, { ...snapshot, revision: versions.current.get(snapshot.id) ?? snapshot.revision });
        versions.current.set(saved.id, saved.revision ?? 0);
        if (current.current?.id === saved.id) {
          current.current = { ...current.current, revision: saved.revision };
          if (editVersion.current === snapshotVersion) dirty.current = false;
          if (available()) setDraft(current.current);
        }
        if (available()) {
          setDrafts(rows => [saved, ...rows.filter(row => row.id !== saved.id)]);
          setStatus(dirty.current ? "Unsaved Changes" : "Saved in This Browser"); setError(undefined);
        }
      });
      pending.current = write;
    }
    try { await write; }
    catch (failure) {
      if (snapshot && current.current?.id === snapshot.id) dirty.current = true;
      reportFailure(failure); throw failure;
    }
    if (snapshot && !options.localOnly && available()) {
      const syncing = syncOne(snapshot.id, snapshotVersion, options.requireRemote);
      if (options.requireRemote) await syncing;
      else void syncing;
    } else if (options.requireRemote) throw new Error("Open a Draft Before Syncing.");
  }, [did, requireViewer, reportFailure, available, syncOne]);

  const update = useCallback((patch: Partial<ArticleDraft>) => {
    requireViewer();
    if (!current.current) return;
    editVersion.current++;
    const timestamp = Math.max(Date.now(), Date.parse(current.current.updatedAt) + 1);
    current.current = { ...current.current, ...patch, updatedAt: new Date(timestamp).toISOString() };
    setDraft(current.current); dirty.current = true; setStatus("Unsaved Changes");
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => { void persist().catch(() => {}); }, 800);
  }, [persist, requireViewer]);

  const select = useCallback(async (id: string) => {
    requireViewer();
    const selection = ++intent.current, selectedVersion = editVersion.current;
    const previousId = current.current?.id, previousDirty = dirty.current;
    let context: ArticleDraftActor | undefined;
    try {
      // Opening a saved draft must not wait for the private sync queue. Preserve
      // outgoing edits locally, then reconcile each affected draft in the background.
      await persist({ localOnly: true });
      context = actor();
      const cached = await getArticleDraft(did, id);
      context?.assertCurrentActor?.();
      if (!available() || selection !== intent.current || selectedVersion !== editVersion.current) return;
      const online = navigator.onLine !== false;
      if (context && online && previousDirty && previousId && previousId !== id) void syncOne(previousId, selectedVersion);
      const saved = cached ?? (context && online ? await loadSyncedArticleDraft(context, id) : undefined);
      context?.assertCurrentActor?.();
      if (!available() || selection !== intent.current || selectedVersion !== editVersion.current) return;
      if (!saved) throw new Error("This draft is no longer available. Refresh the draft list.");
      versions.current.set(saved.id, Math.max(versions.current.get(saved.id) ?? 0, saved.revision ?? 0));
      current.current = saved; dirty.current = false;
      setDraft(saved); setStatus("Saved in This Browser"); setError(undefined);
      if (cached && context && online) void syncOne(id, selectedVersion);
    } catch (failure) {
      let currentActor = true;
      try { context?.assertCurrentActor?.(); } catch { currentActor = false; }
      if (currentActor && selection === intent.current) reportFailure(failure);
      throw failure;
    }
  }, [did, persist, actor, syncOne, requireViewer, reportFailure, available]);

  const create = useCallback(async (publicationUri = "") => {
    requireViewer();
    const creation = ++intent.current, selectedVersion = editVersion.current;
    try {
      await persist();
      if (selectedVersion !== editVersion.current) throw new Error("Your draft changed while saving. Save those edits before creating another draft.");
      const now = new Date().toISOString();
      const saved = await saveArticleDraft(did, { id: crypto.randomUUID(), title: "", markdown: "", excerpt: "", path: "", tags: [], publicationUri, createdAt: now, updatedAt: now, assets: [] });
      if (!available()) return saved;
      versions.current.set(saved.id, saved.revision ?? 0);
      setDrafts(rows => [saved, ...rows.filter(row => row.id !== saved.id)]);
      if (creation === intent.current && selectedVersion === editVersion.current) {
        current.current = saved; dirty.current = false; setDraft(saved); setStatus("Saved in This Browser"); setError(undefined);
      }
      void syncOne(saved.id, selectedVersion);
      return saved;
    } catch (failure) { if (creation === intent.current) reportFailure(failure); throw failure; }
  }, [did, persist, syncOne, requireViewer, reportFailure, available]);

  const remove = useCallback(async (id: string) => {
    requireViewer();
    const removal = ++intent.current, selectedVersion = editVersion.current;
    try {
      await persist();
      if (selectedVersion !== editVersion.current) throw new Error("Your draft changed while saving. Save those edits before deleting a draft.");
      const context = actor();
      if (context) {
        try { await removeSyncedArticleDraft(context, id); }
        catch (failure) { if (await getArticleDraft(did, id)) throw failure; reportFailure(failure, true); }
      } else await deleteArticleDraft(did, id);
      versions.current.delete(id);
      if (!available()) return;
      setDrafts(rows => rows.filter(row => row.id !== id));
      if (current.current?.id === id && removal === intent.current) { current.current = undefined; setDraft(undefined); dirty.current = false; }
    } catch (failure) { if (removal === intent.current) reportFailure(failure); throw failure; }
  }, [did, persist, actor, requireViewer, reportFailure, available]);

  const refresh = useCallback(async () => {
    if (!actorFactory.current || !available()) return;
    const version = editVersion.current, selectedId = current.current?.id;
    let started = false;
    let context: ArticleDraftActor | undefined;
    try {
      await persist({ localOnly: true });
      context = actor(); if (!context) return;
      networkCount.current++; started = true; setSyncing(true);
      await syncArticleDrafts(context);
      context.assertCurrentActor?.();
      // A local write can finish after the cloud operation takes its list
      // snapshot. Paint the current journal and never rewind its CAS revision.
      const rows = await listArticleDrafts(boundDid);
      context.assertCurrentActor?.();
      for (const row of rows) versions.current.set(row.id, Math.max(versions.current.get(row.id) ?? 0, row.revision ?? 0));
      setDrafts(rows);
      if (selectedId) await install(rows.find(row => row.id === selectedId), selectedId, version, context.assertCurrentActor);
      if (available()) { setStatus(dirty.current ? "Unsaved Changes" : "Synced Privately"); setError(undefined); }
    } catch (failure) {
      let currentActor = true;
      try { context?.assertCurrentActor?.(); } catch { currentActor = false; }
      if (currentActor) reportFailure(failure, true);
    }
    finally { if (started) { networkCount.current = Math.max(0, networkCount.current - 1); if (available()) setSyncing(networkCount.current > 0); } }
  }, [actor, available, boundDid, persist, install, reportFailure]);

  useEffect(() => {
    if (did !== boundDid) return;
    mounted.current = true; lifetime.current = new AbortController();
    let cancelled = false;
    const loadIntent = intent.current;
    void listArticleDrafts(did).then(rows => {
      if (cancelled || loadIntent !== intent.current) return;
      setDrafts(rows);
      if (rows[0]) { current.current = rows[0]; versions.current.set(rows[0].id, rows[0].revision ?? 0); setDraft(rows[0]); }
      setStatus("Saved in This Browser"); initialized.current = true;
      void refresh();
    }).catch(failure => { if (!cancelled && loadIntent === intent.current) reportFailure(failure); }).finally(() => { if (!cancelled) setLoading(false); });
    const onVisibility = () => { if (document.visibilityState === "hidden") void persist({ localOnly: true }).catch(() => {}); else void refresh(); };
    const onOnline = () => { void refresh(); };
    document.addEventListener("visibilitychange", onVisibility); window.addEventListener("online", onOnline);
    return () => { cancelled = true; mounted.current = false; initialized.current = false; lifetime.current.abort(); document.removeEventListener("visibilitychange", onVisibility); window.removeEventListener("online", onOnline); void persist({ localOnly: true }).catch(() => {}); };
  }, [did, persist, refresh, boundDid, reportFailure]);
  useEffect(() => { if (initialized.current) void refresh(); }, [getSyncActor, refresh]);
  const sameViewer = did === boundDid;
  return { drafts: sameViewer ? drafts : [], draft: sameViewer ? draft : undefined, loading, syncing, status, error, update, persist, select, create, remove, refresh };
}
