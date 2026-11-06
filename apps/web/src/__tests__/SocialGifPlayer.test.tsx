import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SocialGifPlayer } from "@/components/Social/SocialGifPlayer";
import { SocialGifEmbed } from "@/components/Social/SocialGifEmbed";

const source = "https://media.example/animation.gif";
const poster = "https://media.example/poster.jpg";
const restores: (() => void)[] = [];
let play: ReturnType<typeof mock<() => Promise<void>>>;
let pause: ReturnType<typeof mock<() => void>>;
beforeEach(() => {
  const raf = Object.getOwnPropertyDescriptor(globalThis, "requestAnimationFrame");
  const cancel = Object.getOwnPropertyDescriptor(globalThis, "cancelAnimationFrame");
  Object.defineProperty(globalThis, "requestAnimationFrame", { configurable: true, value: (callback: FrameRequestCallback) => Number(setTimeout(() => callback(performance.now()), 0)) });
  Object.defineProperty(globalThis, "cancelAnimationFrame", { configurable: true, value: (id: number) => clearTimeout(id) });
  restores.push(() => { if (raf) Object.defineProperty(globalThis, "requestAnimationFrame", raf); else Reflect.deleteProperty(globalThis, "requestAnimationFrame"); }, () => { if (cancel) Object.defineProperty(globalThis, "cancelAnimationFrame", cancel); else Reflect.deleteProperty(globalThis, "cancelAnimationFrame"); });
  play = mock(async () => {}); pause = mock(() => {});
  const playing = spyOn(window.HTMLMediaElement.prototype, "play").mockImplementation(play);
  const pausing = spyOn(window.HTMLMediaElement.prototype, "pause").mockImplementation(pause);
  restores.push(() => playing.mockRestore(), () => pausing.mockRestore());
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });

it("autoplays muted looping video inline and keeps manual playback controls", async () => {
  const view = render(<SocialGifPlayer source={source} poster={poster} mp4="https://media.example/animation.mp4" alt="A Wave" />);
  const video = view.container.querySelector("video")!;
  expect(video.autoplay).toBe(true); expect(video.muted).toBe(true); expect(video.loop).toBe(true); expect(video.playsInline).toBe(true);
  await waitFor(() => expect(play).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole("button", { name: "Pause GIF" }));
  expect(pause).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Play GIF" }));
  await waitFor(() => expect(play).toHaveBeenCalledTimes(2));
});

it("starts image-only GIFs without a click and pauses to their poster", () => {
  render(<SocialGifPlayer source={source} poster={poster} alt="A Wave" />);
  expect(screen.getByAltText("A Wave").getAttribute("src")).toBe(source);
  fireEvent.click(screen.getByRole("button", { name: "Pause GIF" }));
  expect(screen.getByAltText("A Wave").getAttribute("src")).toBe(poster);
});

it("keeps one media frame through loading, posterless pause, and playback errors", async () => {
  const view = render(<SocialGifPlayer source={source} alt="A Wave" width={280} height={498} />);
  const frame = view.container.querySelector<HTMLElement>("[data-gif-viewport]")!;
  const checkFrame = () => {
    expect(view.container.querySelector("[data-gif-viewport]")).toBe(frame);
    expect(frame.style.aspectRatio).toBe("280 / 498");
    expect(frame.style.maxHeight).toBe("60dvh");
  };
  checkFrame();
  expect(frame.querySelector("img")?.classList.contains("absolute")).toBe(true);
  fireEvent.load(screen.getByAltText("A Wave")); checkFrame();
  fireEvent.click(screen.getByRole("button", { name: "Pause GIF" }));
  expect(frame.textContent).toContain("Animated GIF"); checkFrame();
  fireEvent.click(screen.getByRole("button", { name: "Play GIF" }));
  fireEvent.error(await screen.findByAltText("A Wave"));
  expect(screen.getByRole("alert").parentElement).toBe(frame);
  expect(screen.getByRole("alert").classList.contains("absolute")).toBe(true);
  checkFrame();
});

it("reserves a square fallback when GIF dimensions are absent or invalid", () => {
  const view = render(<SocialGifPlayer source={source} alt="A Wave" />);
  expect(view.container.querySelector<HTMLElement>("[data-gif-viewport]")?.style.aspectRatio).toBe("1 / 1");
  view.rerender(<SocialGifPlayer source={source} alt="A Wave" width={Infinity} height={0} />);
  expect(view.container.querySelector<HTMLElement>("[data-gif-viewport]")?.style.aspectRatio).toBe("1 / 1");
});

it("opens the GIF viewer from the media without a separate text launcher", async () => {
  const view = render(<SocialGifEmbed external={{ uri: source, title: "A Wave", description: "" }} image={{ thumb: source, fullsize: source, alt: "A Wave" }} />);
  const target = screen.getByRole("button", { name: "Open GIF Viewer" });
  expect(target.querySelector("img")).toBeTruthy();
  expect(target.textContent).toBe("");
  expect(view.container.textContent).not.toContain("Open GIF Viewer");
  fireEvent.click(target);
  expect(await screen.findByRole("dialog")).toBeTruthy();
});

it("pauses offscreen, resumes onscreen, and preserves a manual pause", async () => {
  let intersect!: IntersectionObserverCallback;
  const disconnect = mock(() => {});
  const original = Object.getOwnPropertyDescriptor(globalThis, "IntersectionObserver");
  class Observer {
    constructor(callback: IntersectionObserverCallback) { intersect = callback; }
    observe() {}
    disconnect = disconnect;
  }
  Object.defineProperty(globalThis, "IntersectionObserver", { configurable: true, value: Observer });
  restores.push(() => { if (original) Object.defineProperty(globalThis, "IntersectionObserver", original); else Reflect.deleteProperty(globalThis, "IntersectionObserver"); });
  const view = render(<SocialGifPlayer source={source} mp4="https://media.example/animation.mp4" alt="A Wave" />);
  const frame = view.container.querySelector<HTMLElement>("[data-gif-viewport]")!;
  const frameStyle = frame.getAttribute("style");
  const visibility = async (visible: boolean) => act(async () => intersect([{ isIntersecting: visible } as IntersectionObserverEntry], {} as IntersectionObserver));
  await visibility(true); expect(play).toHaveBeenCalledTimes(1);
  await visibility(false); expect(pause).toHaveBeenCalledTimes(1);
  await visibility(true); expect(play).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole("button", { name: "Pause GIF" }));
  await visibility(false); await visibility(true);
  expect(play).toHaveBeenCalledTimes(2);
  expect(frame.getAttribute("style")).toBe(frameStyle);
  view.unmount(); expect(disconnect).toHaveBeenCalledTimes(1);
});

it("keeps Play available when the browser blocks autoplay", async () => {
  play.mockRejectedValue(new Error("NotAllowedError"));
  render(<SocialGifPlayer source={source} mp4="https://media.example/animation.mp4" alt="A Wave" />);
  expect(await screen.findByRole("button", { name: "Play GIF" })).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  play.mockResolvedValue();
  fireEvent.click(screen.getByRole("button", { name: "Play GIF" }));
  expect(await screen.findByRole("button", { name: "Pause GIF" })).toBeTruthy();
});
