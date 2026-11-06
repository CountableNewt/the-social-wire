import { afterEach, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { createRef, useContext, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { SocialFeedViewportBoundary } from "@/components/Social/SocialFeedViewportBoundary";
import { SocialFeedViewportContext } from "@/components/Social/SocialFeedViewportContext";

afterEach(cleanup);

type Row = { id: string; height: number };
const rows: Row[] = Array.from({ length: 61 }, (_, index) => ({ id: `post-${index}`, height: 100 }));

function Fixture({ items, scope = "did:plc:viewer:following", resetKey = 0, nativeAdjustment = 0 }: {
  items: Row[]; scope?: string; resetKey?: number; nativeAdjustment?: number;
}) {
  // The reference remains stable across updates just like useSocialFeedScroll.
  const container = useLayoutContainer();
  return <div ref={container} data-testid="scroller">
    <SocialFeedViewportBoundary container={container} scope={scope} resetKey={resetKey}>
      <LayoutRows items={items} />
      <NativeAdjustment container={container} amount={nativeAdjustment} />
    </SocialFeedViewportBoundary>
  </div>;
}

function LayoutRows({ items }: { items: Row[] }) {
  return items.map(row => <div key={row.id} data-social-feed-row={row.id} data-height={row.height}
        ref={node => {
          if (!node) return;
          node.getBoundingClientRect = () => {
            let offset = 0;
            for (const sibling of node.parentElement!.children) {
              if (sibling === node) break;
              offset += Number((sibling as HTMLElement).dataset.height);
            }
            const top = 50 + offset - node.parentElement!.scrollTop;
            return { top, bottom: top + Number(node.dataset.height) } as DOMRect;
          };
        }}>{row.id}</div>);
}

function useLayoutContainer() {
  const container = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    container.current!.getBoundingClientRect = () => ({ top: 50 }) as DOMRect;
  }, []);
  return container;
}

function NativeAdjustment({ container, amount }: { container: RefObject<HTMLDivElement | null>; amount: number }) {
  useLayoutEffect(() => simulateNativeScroll(container.current, amount), [amount, container]);
  return null;
}

function simulateNativeScroll(node: HTMLElement | null, amount: number) {
  if (node) node.scrollTop += amount;
}

function NestedPreview() {
  const viewport = useContext(SocialFeedViewportContext)!;
  const [height, setHeight] = useState(100);
  return <SocialFeedViewportBoundary {...viewport}>
    <LayoutRows items={rows.map((row, index) => index === 0 ? { ...row, height } : row)} />
    <button data-height="0" onClick={() => setHeight(400)}>Resolve Article Metadata</button>
  </SocialFeedViewportBoundary>;
}

function NestedFixture() {
  const container = useLayoutContainer();
  return <div ref={container} data-testid="scroller">
    <SocialFeedViewportBoundary container={container} scope="did:plc:viewer:following" resetKey={0}>
      <NestedPreview />
    </SocialFeedViewportBoundary>
  </div>;
}

it("preserves the visible offset when a nested article preview resolves without a timeline update", () => {
  const view = render(<NestedFixture />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 2030;
  const offset = view.getByText("post-20").getBoundingClientRect().top;
  fireEvent.click(view.getByText("Resolve Article Metadata"));
  expect(container.scrollTop).toBe(2330);
  expect(view.getByText("post-20").getBoundingClientRect().top).toBe(offset);
  // A later user scroll becomes the next update's baseline, never an old snapshot.
  container.scrollTop = 3030;
  fireEvent.click(view.getByText("Resolve Article Metadata"));
  expect(container.scrollTop).toBe(3030);
});

it("keeps the visible post offset when deferred parent hydration grows earlier content", () => {
  const view = render(<Fixture items={rows} />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 2030;
  const anchor = view.getByText("post-20");
  const offset = anchor.getBoundingClientRect().top;
  view.rerender(<Fixture items={rows.map((row, index) => index === 0 ? { ...row, height: 600 } : row)} />);
  expect(container.scrollTop).toBe(2530);
  expect(anchor.getBoundingClientRect().top).toBe(offset);
});

function ConversationFixture({ parentHeight, insidePost = false, showSection = true }: { parentHeight: number; insidePost?: boolean; showSection?: boolean }) {
  const container = useLayoutContainer();
  return <div ref={container} data-testid="scroller">
    <SocialFeedViewportBoundary container={container} scope="viewer:following" resetKey={0}>
      <div data-social-feed-row="reply-row" data-parent-height={parentHeight} ref={node => {
        if (node) node.getBoundingClientRect = () => ({ top: 50 - node.parentElement!.scrollTop,
          bottom: 1050 + Number(node.dataset.parentHeight) - node.parentElement!.scrollTop }) as DOMRect;
      }}>
        <article data-analytics-subject="reply" ref={node => {
          if (node) node.getBoundingClientRect = () => {
            const top = 50 + (insidePost ? 0 : Number(node.parentElement!.dataset.parentHeight)) - node.parentElement!.parentElement!.scrollTop;
            return { top, bottom: top + 1000 } as DOMRect;
          };
        }}>Visible Reply{showSection ? <div data-social-feed-section="actions" ref={node => {
          if (node) node.getBoundingClientRect = () => {
            const top = 50 + Number(node.parentElement!.parentElement!.dataset.parentHeight) - container.current!.scrollTop;
            return { top, bottom: top + 300 } as DOMRect;
          };
        }}>Visible Actions</div> : null}</article>
      </div>
    </SocialFeedViewportBoundary>
  </div>;
}

it("preserves the reply itself when its newly hydrated parent grows inside the same conversation row", () => {
  const view = render(<ConversationFixture parentHeight={200} />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 180;
  const offset = view.getByText("Visible Reply").getBoundingClientRect().top;
  view.rerender(<ConversationFixture parentHeight={600} />);
  expect(container.scrollTop).toBe(580);
  expect(view.getByText("Visible Reply").getBoundingClientRect().top).toBe(offset);
});

it("preserves the visible section when async content grows within the same tall post", () => {
  const view = render(<ConversationFixture parentHeight={200} insidePost />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 230;
  const offset = view.getByText("Visible Actions").getBoundingClientRect().top;
  view.rerender(<ConversationFixture parentHeight={600} insidePost />);
  expect(container.scrollTop).toBe(630);
  expect(view.getByText("Visible Actions").getBoundingClientRect().top).toBe(offset);
});

it("falls back to the existing post offset when the named section is removed", () => {
  const view = render(<ConversationFixture parentHeight={200} insidePost />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 230;
  const offset = view.getByText("Visible Reply").getBoundingClientRect().top;
  view.rerender(<ConversationFixture parentHeight={600} insidePost showSection={false} />);
  expect(container.scrollTop).toBe(230);
  expect(view.getByText("Visible Reply").getBoundingClientRect().top).toBe(offset);
});

it("keeps the viewport when Load More groups an earlier parent into a later reply", () => {
  const view = render(<Fixture items={rows} />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 2030;
  const anchor = view.getByText("post-20");
  const offset = anchor.getBoundingClientRect().top;
  view.rerender(<Fixture items={[...rows.slice(1), { id: "later-reply", height: 500 }]} />);
  expect(container.scrollTop).toBe(1930);
  expect(anchor.getBoundingClientRect().top).toBe(offset);
});

it("uses a surviving following row when grouping removes the visible row itself", () => {
  const view = render(<Fixture items={rows} />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 2030;
  const offset = view.getByText("post-21").getBoundingClientRect().top;
  view.rerender(<Fixture items={rows.filter(row => row.id !== "post-20")} />);
  expect(view.getByText("post-21").getBoundingClientRect().top).toBe(offset);
});

it("does not double compensate when native scroll anchoring already preserved the post", () => {
  const view = render(<Fixture items={rows} />);
  const container = view.getByTestId("scroller");
  container.scrollTop = 2030;
  view.rerender(<Fixture items={rows.map((row, index) => index === 0 ? { ...row, height: 400 } : row)} nativeAdjustment={300} />);
  expect(container.scrollTop).toBe(2330);
});

it("does not restore an old viewport across explicit refresh, route, or account changes", () => {
  for (const change of [{ resetKey: 1 }, { scope: "did:plc:viewer:custom" }, { scope: "did:plc:other:following" }]) {
    const view = render(<Fixture items={rows} />);
    const container = view.getByTestId("scroller");
    container.scrollTop = 2030;
    view.rerender(<Fixture items={rows.slice(1)} {...change} />);
    expect(container.scrollTop).toBe(2030);
    view.unmount();
  }
});

it("keeps an unscrolled feed at the top when new content is inserted", () => {
  const view = render(<Fixture items={rows} />);
  view.rerender(<Fixture items={[{ id: "new", height: 500 }, ...rows]} />);
  expect(view.getByTestId("scroller").scrollTop).toBe(0);
});

it("keeps viewport context stable when parent feed children change", () => {
  const observed = mock(() => {});
  function Probe() {
    useContext(SocialFeedViewportContext);
    observed();
    return null;
  }
  const probe = <Probe />;
  const container = createRef<HTMLDivElement>();
  const tree = (tick: number) => <div ref={container}>
    <SocialFeedViewportBoundary container={container} scope="viewer:following" resetKey={0}>
      <span>{tick}</span>{probe}
    </SocialFeedViewportBoundary>
  </div>;
  const view = render(tree(0));
  view.rerender(tree(1));
  expect(observed).toHaveBeenCalledTimes(1);
});
