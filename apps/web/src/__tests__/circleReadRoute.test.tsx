import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import * as Navigation from "next/navigation";
import * as WireHooks from "@/hooks/useWireFeed";
import * as CircleHooks from "@/hooks/useCircleFeed";
import * as ReadPublicationPage from "@/app/read/[...pubId]/ReadPubPage";
import ReadIndexPage from "@/app/read/page";
import { CircleNewsExperience } from "@/components/Circle/CircleNewsExperience";

const originalAlgorithmicFlag = process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
let route = "circle";
const replace = mock(() => undefined);
const refetchCatalog = mock(() => Promise.resolve());
const refetchEdition = mock(() => Promise.resolve());
const restores: Array<() => void> = [];
let state: ReturnType<typeof CircleHooks.useCircleEdition>;

describe("Circle read route integration", () => {
  beforeEach(() => {
    process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "true";
    route = "circle";
    replace.mockClear();
    refetchCatalog.mockClear();
    refetchEdition.mockClear();
    state = {
      catalog: {
        data: { enabled: true, available: false },
        isLoading: false, isError: false, refetch: refetchCatalog,
      },
      data: undefined,
      isLoading: false, isRefetching: false, isError: false,
      refetch: refetchEdition,
    } as unknown as typeof state;
    const search = spyOn(Navigation, "useSearchParams").mockImplementation(() =>
      new URLSearchParams(`feed=${route}`) as ReturnType<typeof Navigation.useSearchParams>);
    const router = spyOn(Navigation, "useRouter").mockReturnValue(
      { replace } as unknown as ReturnType<typeof Navigation.useRouter>);
    const wire = spyOn(WireHooks, "useWireFeedCatalog").mockReturnValue(
      { data: { enabled: false, available: false }, isLoading: false } as ReturnType<typeof WireHooks.useWireFeedCatalog>);
    const circleCatalog = spyOn(CircleHooks, "useCircleCatalog").mockImplementation(() => state.catalog);
    const circleEdition = spyOn(CircleHooks, "useCircleEdition").mockImplementation(() => state);
    // Keep the actual route's Circle flag and content while isolating unrelated reader panes.
    const publication = spyOn(ReadPublicationPage, "default").mockImplementation(({ circleFeed }) =>
      circleFeed ? <CircleNewsExperience onSelect={() => undefined} /> : <p>Other Feed</p>);
    restores.push(...[search, router, wire, circleCatalog, circleEdition, publication].map((spy) => () => spy.mockRestore()));
  });

  afterEach(() => {
    if (originalAlgorithmicFlag === undefined) delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
    else process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = originalAlgorithmicFlag;
    cleanup();
    for (const restore of restores.splice(0).reverse()) restore();
  });

  for (const feed of ["wire", "circle", "finance", "sports"]) {
    it(`redirects a suspended ${feed} route before mounting its experience`, async () => {
      process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "false";
      route = feed;
      await act(async () => { render(<ReadIndexPage />); });
      expect(screen.getByText("Other Feed")).toBeDefined();
      expect(replace).toHaveBeenCalledWith("/read?feed=subscribed");
      expect(WireHooks.useWireFeedCatalog).toHaveBeenLastCalledWith({ enabled: false });
      expect(CircleHooks.useCircleEdition).not.toHaveBeenCalled();
    });
  }

  it("lets unavailable corpus reach the explanatory Circle view and retry its catalog", async () => {
    await act(async () => { render(<ReadIndexPage />); });
    expect(screen.getByRole("heading", { name: "Your Circle Is Not Ready Yet" })).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetchCatalog).toHaveBeenCalledTimes(1);
    expect(refetchEdition).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("keeps a failed direct Circle route on its retryable error state", async () => {
    state.catalog.data = undefined;
    state.catalog.isError = true;
    await act(async () => { render(<ReadIndexPage />); });
    expect(screen.getByRole("heading", { name: "Your Circle Could Not Load" })).toBeDefined();
    expect(replace).not.toHaveBeenCalled();
  });

  it("does not bypass explicit global disablement on a direct Circle route", async () => {
    state.catalog.data!.enabled = false;
    await act(async () => { render(<ReadIndexPage />); });
    expect(screen.getByRole("heading", { name: "Your Circle Is Unavailable" })).toBeDefined();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(refetchEdition).not.toHaveBeenCalled();
  });
});
