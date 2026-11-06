"use client";

import { Component, type ReactNode } from "react";
import { SocialFeedViewportContext, type SocialFeedViewport } from "./SocialFeedViewportContext";

interface Props extends SocialFeedViewport {
  children: ReactNode;
}

interface Snapshot {
  container: HTMLDivElement;
  anchors: { identity: string; subject?: string; offset: number }[];
}

export function resetSocialFeedViewport(container: HTMLElement | null) {
  if (container) container.scrollTop = 0;
}

/** Capture before React changes rows; layout-effect cleanup runs too late. */
export class SocialFeedViewportBoundary extends Component<Props, object, Snapshot | null> {
  private viewport?: SocialFeedViewport;

  getSnapshotBeforeUpdate(previous: Props): Snapshot | null {
    const container = this.props.container.current;
    if (previous.children === this.props.children || !container || container !== previous.container.current || previous.scope !== this.props.scope
      || previous.resetKey !== this.props.resetKey || container.scrollTop <= 0) return null;
    const top = container.getBoundingClientRect().top;
    const anchors: Snapshot["anchors"] = [];
    for (const row of container.querySelectorAll<HTMLElement>("[data-social-feed-row]")) {
      const rect = row.getBoundingClientRect();
      if (rect.bottom <= top) continue;
      const posts = [...row.querySelectorAll<HTMLElement>("[data-analytics-subject]")];
      // A newly hydrated parent can move the reply inside its existing row.
      const post = posts.find(post => post.getBoundingClientRect().bottom > top);
      anchors.push({
        identity: row.dataset.socialFeedRow!,
        subject: post?.dataset.analyticsSubject,
        offset: (post ?? row).getBoundingClientRect().top - top,
      });
      // Keep following rows as fallbacks when grouping removes the visible row.
      if (anchors.length === 8) break;
    }
    return { container, anchors };
  }

  componentDidUpdate(_previous: Props, _state: object, snapshot: Snapshot | null) {
    if (!snapshot || this.props.container.current !== snapshot.container) return;
    const container = snapshot.container;
    const rows = new Map([...container.querySelectorAll<HTMLElement>("[data-social-feed-row]")]
      .map(row => [row.dataset.socialFeedRow, row]));
    for (const anchor of snapshot.anchors) {
      const row = rows.get(anchor.identity);
      if (!row) continue;
      const post = anchor.subject ? [...row.querySelectorAll<HTMLElement>("[data-analytics-subject]")]
        .find(post => post.dataset.analyticsSubject === anchor.subject) : row;
      if (!post) continue;
      const delta = post.getBoundingClientRect().top - container.getBoundingClientRect().top - anchor.offset;
      // Browser scroll anchoring may already have preserved this exact offset.
      if (Math.abs(delta) > 0.5) container.scrollTop += delta;
      break;
    }
  }

  render() {
    const { container, scope, resetKey } = this.props;
    if (!this.viewport || this.viewport.container !== container || this.viewport.scope !== scope || this.viewport.resetKey !== resetKey) {
      this.viewport = { container, scope, resetKey };
    }
    return <SocialFeedViewportContext.Provider value={this.viewport}>{this.props.children}</SocialFeedViewportContext.Provider>;
  }
}
