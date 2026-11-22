import { afterEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import * as Navigation from "next/navigation";
import ReadIndexPage from "@/app/read/page";
import * as ReadPubPage from "@/app/read/[...pubId]/ReadPubPage";

const restores: (() => void)[] = [];
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });

it.each(["wire", "circle", "finance", "sports"])("redirects a stale %s URL to Subscribed", async feed => {
  const params = spyOn(Navigation, "useSearchParams").mockReturnValue(new URLSearchParams(`feed=${feed}`) as ReturnType<typeof Navigation.useSearchParams>);
  const redirects: string[] = [];
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace: (path: string) => redirects.push(path) } as unknown as ReturnType<typeof Navigation.useRouter>);
  const mounted: unknown[] = [];
  const reader = spyOn(ReadPubPage, "default").mockImplementation(({ aggregateFeed }) => { mounted.push(aggregateFeed); return <div>Subscribed Reader</div>; });
  restores.push(...[params, router, reader].map(spy => () => spy.mockRestore()));
  await act(async () => { render(<ReadIndexPage />); });
  await waitFor(() => expect(redirects).toContain("/read?feed=subscribed"));
  expect(mounted.length).toBeGreaterThan(0);
  expect(mounted.every(value => JSON.stringify(value) === JSON.stringify({ kind: "subscribed" }))).toBe(true);
});

it("shows an unavailable state for unknown feeds", async () => {
  const params = spyOn(Navigation, "useSearchParams").mockReturnValue(new URLSearchParams("feed=unknown") as ReturnType<typeof Navigation.useSearchParams>);
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace() {} } as unknown as ReturnType<typeof Navigation.useRouter>);
  restores.push(() => params.mockRestore(), () => router.mockRestore());
  const view = render(<ReadIndexPage />);
  await waitFor(() => expect(view.getByText("This feed is unavailable.")).toBeTruthy());
});
