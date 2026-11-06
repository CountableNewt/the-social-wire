import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import * as Cache from "@/lib/imageBlobCache";
import { CachedImage } from "@/components/shared/CachedImage";

const originalObserver = Object.getOwnPropertyDescriptor(globalThis, "IntersectionObserver");
const observers: { callback: IntersectionObserverCallback; target?: Element }[] = [];
const restores: (() => void)[] = [];
beforeEach(() => {
  observers.length = 0;
  Object.defineProperty(globalThis, "IntersectionObserver", { configurable: true, value: class {
    entry: typeof observers[number];
    constructor(callback: IntersectionObserverCallback) { this.entry = { callback }; observers.push(this.entry); }
    observe(target: Element) { this.entry.target = target; }
    disconnect() {}
  } });
  const revoke = spyOn(URL, "revokeObjectURL").mockImplementation(() => {}); restores.push(() => revoke.mockRestore());
});
afterEach(() => {
  cleanup(); restores.splice(0).reverse().forEach(restore => restore());
  if (originalObserver) Object.defineProperty(globalThis, "IntersectionObserver", originalObserver); else Reflect.deleteProperty(globalThis, "IntersectionObserver");
});
const source = "/api/images/one.png";

it("keeps offscreen images out of the fetch queue while reserving their dimensions", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValue("blob:visible"); restores.push(() => fetch.mockRestore());
  const view = render(<>{Array.from({ length: 20 }, (_, i) => <CachedImage key={i} src={`/api/images/${i}.png`} alt={`Image ${i}`} width={100} height={80} />)}</>);
  expect(fetch).not.toHaveBeenCalled();
  const images = view.getAllByRole("img", { hidden: true });
  expect(images).toHaveLength(20);
  expect(images[0].getAttribute("width")).toBe("100");
  expect(images[0].getAttribute("height")).toBe("80");
  expect(images[0].getAttribute("src")).toBeNull();
  const rect = images[0].getBoundingClientRect();
  act(() => observers[0].callback([{ time: 0, isIntersecting: true, target: images[0], rootBounds: null, boundingClientRect: rect, intersectionRect: rect, intersectionRatio: 1 }], {} as IntersectionObserver));
  await waitFor(() => expect(images[0].getAttribute("src")).toBe("blob:visible"));
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("invalidates a failed decode and retries once with fresh HTTP bytes", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValueOnce("blob:bad").mockResolvedValueOnce("blob:good"); restores.push(() => fetch.mockRestore());
  const invalidate = spyOn(Cache, "invalidateCachedImage").mockResolvedValue(); restores.push(() => invalidate.mockRestore());
  let failures = 0, loads = 0;
  const view = render(<CachedImage src={source} alt="Photo" width={100} height={80} loading="eager" onError={() => failures++} onLoad={() => loads++} />);
  const image = view.getByRole("img", { hidden: true });
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:bad"));
  fireEvent.error(image);
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:good"));
  expect(invalidate).toHaveBeenCalledWith(source);
  expect(fetch.mock.calls[1][2]).toEqual({ reload: true });
  expect(failures).toBe(0);
  fireEvent.load(image); expect(loads).toBe(1);
});

it("ignores a queued visibility callback for an image that has been replaced", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValue("blob:current"); restores.push(() => fetch.mockRestore());
  const view = render(<CachedImage src={source} alt="Photo" width={100} height={80} />);
  const image = view.getByRole("img", { hidden: true });
  const retired = observers[0];
  view.rerender(<CachedImage src="/api/images/current.png" alt="Photo" width={100} height={80} />);
  const current = observers[1];
  const rect = image.getBoundingClientRect();
  const entry = { time: 0, isIntersecting: true, target: image, rootBounds: null, boundingClientRect: rect, intersectionRect: rect, intersectionRatio: 1 };
  act(() => current.callback([entry], {} as IntersectionObserver));
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:current"));
  act(() => retired.callback([entry], {} as IntersectionObserver));
  expect(image.getAttribute("src")).toBe("blob:current");
  expect(fetch).toHaveBeenCalledTimes(1);
});

it("bounds a repeated decode failure without collapsing the image slot", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValue("blob:bad"); restores.push(() => fetch.mockRestore());
  const invalidate = spyOn(Cache, "invalidateCachedImage").mockResolvedValue(); restores.push(() => invalidate.mockRestore());
  let failures = 0;
  const view = render(<CachedImage src={source} alt="Photo" width={100} height={80} loading="eager" onError={() => failures++} />);
  const image = view.getByRole("img", { hidden: true });
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:bad"));
  fireEvent.error(image);
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:bad"));
  fireEvent.error(image);
  await waitFor(() => expect(failures).toBe(1));
  expect(image.isConnected).toBe(true);
  expect(image.getAttribute("width")).toBe("100"); expect(image.getAttribute("height")).toBe("80");
  expect(image.getAttribute("src")).toBeNull();
  fireEvent.error(image); expect(failures).toBe(1); expect(fetch).toHaveBeenCalledTimes(2);
});

it("cannot retry an old source after the component changes images", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValueOnce("blob:first").mockResolvedValueOnce("blob:second"); restores.push(() => fetch.mockRestore());
  let finish!: () => void;
  const invalidate = spyOn(Cache, "invalidateCachedImage").mockReturnValue(new Promise<void>(resolve => { finish = resolve; })); restores.push(() => invalidate.mockRestore());
  const view = render(<CachedImage src={source} alt="Photo" width={100} height={80} loading="eager" />);
  const image = view.getByRole("img", { hidden: true });
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:first"));
  fireEvent.error(image);
  view.rerender(<CachedImage src="/api/images/two.png" alt="Photo" width={100} height={80} loading="eager" />);
  await waitFor(() => expect(image.getAttribute("src")).toBe("blob:second"));
  await act(async () => finish());
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(image.getAttribute("src")).toBe("blob:second");
});

it("does not repeatedly invalidate a failed image when its parent rerenders", async () => {
  const fetch = spyOn(Cache, "fetchCachedImageObjectUrl").mockResolvedValue(undefined); restores.push(() => fetch.mockRestore());
  const invalidate = spyOn(Cache, "invalidateCachedImage").mockResolvedValue(); restores.push(() => invalidate.mockRestore());
  let failures = 0;
  const view = render(<CachedImage src={source} alt="Photo" width={100} height={80} loading="eager" onError={() => failures++} />);
  await waitFor(() => expect(failures).toBe(1));
  expect(invalidate).toHaveBeenCalledTimes(2);
  view.rerender(<CachedImage src={source} alt="Photo" width={100} height={80} loading="eager" onError={() => failures++} />);
  expect(invalidate).toHaveBeenCalledTimes(2);
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(failures).toBe(1);
});
