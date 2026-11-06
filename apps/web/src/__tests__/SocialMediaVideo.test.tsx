import { useRef, useState } from "react";
import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComposerMedia, ComposerVideo } from "@/lib/composerMedia";
import { SocialMediaControls } from "@/components/Social/SocialMediaControls";
import * as preparation from "@/lib/prepareComposerVideo";

const restores: (() => void)[] = [];
beforeEach(() => {
  const values = { getComputedStyle: window.getComputedStyle.bind(window), requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0), cancelAnimationFrame: clearTimeout };
  for (const [name, value] of Object.entries(values)) {
    const previous = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
    restores.push(() => { if (previous) Object.defineProperty(globalThis, name, previous); else Reflect.deleteProperty(globalThis, name); });
  }
  const load = spyOn(window.HTMLMediaElement.prototype, "load").mockImplementation(() => {});
  const pause = spyOn(window.HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  restores.push(() => load.mockRestore(), () => pause.mockRestore());
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });
function Harness({ initial, disabled = false }: { initial?: ComposerMedia; disabled?: boolean }) {
  const [media, setMedia] = useState(initial);
  const intake = useRef<((files: File[]) => void) | undefined>(undefined);
  return <div onDrop={event => intake.current?.(Array.from(event.dataTransfer.files))}>
    <SocialMediaControls media={media} update={change => setMedia(previous => change(previous))} disabled={disabled} hasLink={false} removeLink={() => {}} registerIntake={intake} />
    <output data-testid="state">{media ? JSON.stringify({ kind: media.kind, ...(media.kind === "video" ? { id: media.id, status: media.status, alt: media.alt } : {}) }) : "empty"}</output>
  </div>;
}
const original = () => new File(["video bytes"], "clip.mp4", { type: "video/mp4" });
const ready = (video: ComposerVideo): ComposerVideo => ({ ...video, blob: video.original, status: "ready", width: 720, height: 1280, duration: 5 });
it("selects a video, edits alt text, and removes its preview", async () => {
  const prepare = spyOn(preparation, "prepareComposerVideo").mockImplementation(async video => ready(video)); restores.push(() => prepare.mockRestore());
  render(<Harness />);
  fireEvent.change(screen.getByLabelText("Choose Video"), { target: { files: [original()] } });
  await waitFor(() => expect(screen.getByTestId("state").textContent).toContain('"status":"ready"'));
  expect(screen.getByLabelText("Preview clip.mp4").hasAttribute("controls")).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Add Alt Text" }));
  expect(screen.getByRole("dialog").textContent).toContain("Describe This Video");
  expect(screen.getByLabelText("Alt Text").getAttribute("maxlength")).toBe("1000");
  fireEvent.change(screen.getByLabelText("Alt Text"), { target: { value: "A penguin swimming" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Description" }));
  expect(screen.getByLabelText("A penguin swimming")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Remove Video" }));
  expect(screen.getByTestId("state").textContent).toContain("empty");
});
it("retains a failed file for retry and ignores late preparation after removal", async () => {
  let resolve!: (video: ComposerVideo) => void;
  let pending!: ComposerVideo;
  const prepare = spyOn(preparation, "prepareComposerVideo").mockRejectedValueOnce(new Error("Unsupported Video"))
    .mockImplementation(video => { pending = video; return new Promise(done => { resolve = done; }); }); restores.push(() => prepare.mockRestore());
  render(<Harness />);
  fireEvent.change(screen.getByLabelText("Choose Video"), { target: { files: [original()] } });
  await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Unsupported Video"));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(screen.getByTestId("state").textContent).toContain('"status":"preparing"');
  fireEvent.click(screen.getByRole("button", { name: "Remove Video" }));
  await act(async () => resolve(ready(pending)));
  expect(screen.getByTestId("state").textContent).toContain("empty");
});
it("requires confirmation before replacing photos and rejects mixed video drops", async () => {
  const prepare = spyOn(preparation, "prepareComposerVideo").mockImplementation(async video => ready(video)); restores.push(() => prepare.mockRestore());
  const { container } = render(<Harness initial={{ kind: "photos", photos: [] }} />);
  fireEvent.drop(container.firstChild!, { dataTransfer: { files: [original(), new File(["photo"], "photo.jpg", { type: "image/jpeg" })] } });
  expect(screen.getByRole("alert").textContent).toContain("Choose One Video"); expect(prepare).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Choose Video"), { target: { files: [original()] } });
  expect(screen.getByRole("dialog").textContent).toContain("Replace Current Attachment?"); expect(prepare).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Replace Attachment" }));
  await waitFor(() => expect(screen.getByTestId("state").textContent).toContain('"status":"ready"'));
});
