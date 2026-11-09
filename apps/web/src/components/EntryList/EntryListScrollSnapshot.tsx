"use client";

import { Component, type ReactNode } from "react";

/** Read the live offset before row refs measure the next commit's content. */
export class EntryListScrollSnapshot extends Component<{
  captureScrollTop: () => void;
  children: ReactNode;
}> {
  getSnapshotBeforeUpdate() {
    this.props.captureScrollTop();
    return null;
  }

  componentDidUpdate() {
    // The pane's layout effect restores the snapshot after virtual measurement.
  }

  render() {
    return this.props.children;
  }
}
