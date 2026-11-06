import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, render, screen } from "@testing-library/react";
import * as Navigation from "next/navigation";
import * as WireHooks from "@/hooks/useWireFeed";
import * as CircleHooks from "@/hooks/useCircleFeed";
import * as ReadPublicationPage from "@/app/read/[...pubId]/ReadPubPage";
import ReadIndexPage from "@/app/read/page";

const restores: (() => void)[] = [];
const replace = mock(() => {});
let query: string;
let previousEnv: string | undefined;
let wire: ReturnType<typeof spyOn>;
let circle: ReturnType<typeof spyOn>;
beforeEach(() => {
  previousEnv = process.env.NEXT_PUBLIC_APP_ENV;
  query = "feed=circle"; replace.mockClear();
  const params = spyOn(Navigation, "useSearchParams").mockImplementation(() => new URLSearchParams(query) as ReturnType<typeof Navigation.useSearchParams>);
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace } as unknown as ReturnType<typeof Navigation.useRouter>);
  wire = spyOn(WireHooks, "useWireFeedCatalog").mockReturnValue({ data: { enabled: true, available: true } } as ReturnType<typeof WireHooks.useWireFeedCatalog>);
  circle = spyOn(CircleHooks, "useCircleEdition");
  const page = spyOn(ReadPublicationPage, "default").mockImplementation(({ wireFeed, circleFeed, aggregateFeed }) => <p>{wireFeed || circleFeed ? "Retired Feed" : aggregateFeed?.kind}</p>);
  restores.push(...[params, router, wire, circle, page].map(spy => () => spy.mockRestore()));
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); if (previousEnv === undefined) delete process.env.NEXT_PUBLIC_APP_ENV; else process.env.NEXT_PUBLIC_APP_ENV = previousEnv; });

describe("retired discovery reader routes", () => {
  it.each(["local", "dev", "prod"].flatMap(environment => ["wire", "circle"].map(feed => [environment, feed])))("in %s replaces retired %s URLs without consulting their catalogs", async (environment, feed) => {
    process.env.NEXT_PUBLIC_APP_ENV = environment;
    query = `feed=${feed}&entry=keep`;
    await act(async () => { render(<ReadIndexPage />); });
    expect(screen.getByText("subscribed")).toBeDefined();
    expect(replace).toHaveBeenCalledWith("/read?feed=subscribed&entry=keep");
    expect(wire).not.toHaveBeenCalled(); expect(circle).not.toHaveBeenCalled();
    expect(screen.queryByText("Retired Feed")).toBeNull();
  });
});
