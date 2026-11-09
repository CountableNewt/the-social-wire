import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render as renderUI, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import * as ProfileLinkClient from "@/lib/profileLinks";
import * as Profiles from "@/hooks/useViewerProfile";
import { ProfileInformation } from "@/components/Account/ProfileInformation";

const originalMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
beforeAll(() => Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }));
afterAll(() => { if (originalMedia) Object.defineProperty(window, "matchMedia", originalMedia); else Reflect.deleteProperty(window, "matchMedia"); });
let state: { data: Profiles.ViewerProfileSlice | null; isLoading: boolean; refetch: ReturnType<typeof mock> };
let restore: () => void;
let cache: QueryClient;
function render(ui: ReactElement) { return renderUI(ui, { wrapper: ({ children }) => <QueryClientProvider client={cache}>{children}</QueryClientProvider> }); }
beforeEach(() => {
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const links = spyOn(ProfileLinkClient, "loadProfileLinks").mockResolvedValue([{ uri: "at://did:plc:viewer/app.bsky.actor.link/site", url: "https://viewer.example/", title: "My Website", domain: "viewer.example" }]);
  state = { data: { did: "did:plc:viewer", displayName: "Local Viewer", handle: "viewer.local.test", description: "First line\nSecond line", followersCount: 12, followsCount: 0, postsCount: 48 }, isLoading: false, refetch: mock(async () => undefined) };
  const profile = spyOn(Profiles, "useViewerProfile").mockImplementation(() => state as unknown as ReturnType<typeof Profiles.useViewerProfile>);
  restore = () => { profile.mockRestore(); links.mockRestore(); };
});
afterEach(() => { cleanup(); cache.clear(); restore(); });

describe("Profile information", () => {
  it("displays the viewer identity, biography, available social counts, and profile link cards", async () => {
    render(<ProfileInformation />);
    expect(screen.getByRole("heading", { name: "Local Viewer" })).toBeTruthy();
    expect(screen.getByText("@viewer.local.test")).toBeTruthy();
    expect(screen.getByText("First line Second line")).toBeTruthy();
    expect(screen.getByText("Followers").nextElementSibling?.textContent).toBe("12");
    expect(screen.getByText("Following").nextElementSibling?.textContent).toBe("0");
    expect(screen.getByText("Posts").nextElementSibling?.textContent).toBe("48");
    expect(screen.getByRole("heading", { name: "Local Viewer" }).closest("section")?.id).toBe("profile");
    const website = await screen.findByRole("link", { name: /My Website/ });
    expect(website.getAttribute("href")).toBe("https://viewer.example/");
    expect(website.getAttribute("target")).toBe("_blank");
    expect(website.getAttribute("rel")).toBe("noopener noreferrer");
    expect(screen.queryByRole("link", { name: "Open on Bluesky" })).toBeNull();
  });
  it("renders native tags and links in the signed-in viewer biography", () => {
    state.data!.description = "Own bio #SocialWire https://example.com";
    render(<ProfileInformation />);
    expect(screen.getByRole("link", { name: "#SocialWire" }).getAttribute("href")).toBe("https://bsky.app/search?q=%23SocialWire");
    expect(screen.getByRole("link", { name: "https://example.com" }).getAttribute("href")).toBe("https://example.com/");
  });
  it("shows trimmed pronouns beside the display name", () => {
    state.data!.pronouns = "  they/them  ";
    render(<ProfileInformation />);
    const heading = screen.getByRole("heading", { name: "Local Viewer they/them" });
    expect(heading.contains(screen.getByText("they/them"))).toBe(true);
    expect(heading.textContent).toBe("Local Viewer they/them");
  });
  it.each([undefined, "", "   "])("omits missing or blank pronouns: %p", pronouns => {
    state.data!.pronouns = pronouns;
    render(<ProfileInformation />);
    expect(screen.getByRole("heading", { name: "Local Viewer" }).textContent).toBe("Local Viewer");
  });
  it("renders pronouns as plain text", () => {
    state.data!.pronouns = "<b>she/her</b>";
    render(<ProfileInformation />);
    const pronouns = screen.getByText("<b>she/her</b>");
    expect(pronouns.children.length).toBe(0);
  });
  it("omits unknown counts and keeps a DID fallback readable", () => {
    state.data = { did: "did:plc:viewer", handle: "did:plc:viewer", description: "Repo fallback profile" };
    render(<ProfileInformation />);
    expect(screen.getByRole("heading", { name: "did:plc:viewer" })).toBeTruthy();
    expect(screen.queryByText("@did:plc:viewer")).toBeNull();
    expect(screen.queryByText("Followers")).toBeNull();
    expect(screen.queryByText("Following")).toBeNull();
  });
  it("omits link cards for a profile with no configured links", async () => {
    const links = spyOn(ProfileLinkClient, "loadProfileLinks").mockResolvedValue([]);
    render(<ProfileInformation />);
    await waitFor(() => expect(cache.getQueryState(["actorProfileLinks", "did:plc:viewer"])?.status).toBe("success"));
    expect(links).toHaveBeenCalledWith("did:plc:viewer", expect.any(AbortSignal));
    expect(screen.queryByRole("link", { name: "Open on Bluesky" })).toBeNull();
    expect(screen.queryByRole("link", { name: /My Website/ })).toBeNull();
  });
  it("shows loading and provides retry when profile retrieval fails", () => {
    state.data = null; state.isLoading = true;
    const view = render(<ProfileInformation />);
    expect(screen.getByRole("status", { name: "Loading Profile" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Open on Bluesky" })).toBeNull();
    state.isLoading = false;
    view.rerender(<ProfileInformation />);
    expect(screen.getByRole("heading", { name: "Profile Unavailable" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Open on Bluesky" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetch).toHaveBeenCalledTimes(1);
  });
});
