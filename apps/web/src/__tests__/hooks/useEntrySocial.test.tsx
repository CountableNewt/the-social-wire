import { describe, expect, it, mock, beforeEach } from "bun:test";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import { useEntrySocial } from "@/hooks/useEntrySocial";
import type { EntryDetail } from "@/lib/atprotoClient";

const postMock = mock(async (record: unknown) => ({
  record,
  uri: "at://did:plc:me/app.bsky.feed.post/1",
}));
const likeMock = mock(async (uri: string, cid: string) => {
  void uri;
  void cid;
});
const getPostsMock = mock(async () => ({ data: { posts: [] } }));
const getTokenInfoMock = mock(async () => ({
  scope: "atproto",
  iss: "https://pds.example",
  aud: "https://pds.example",
  sub: "did:plc:me",
}));

mock.module("@/hooks/useAuth", () => ({
  useAuth: () => ({
    session: { did: "did:plc:me" },
    isLoading: false,
    oauthSessionReloadSeq: 0,
    applyOAuthSession: () => {},
    getOAuthSession: () => ({
      getTokenInfo: getTokenInfoMock,
      fetchHandler: mock(async () => new Response("{}")),
    }),
    getAuthFetch: () => null,
    reconcileOAuthSession: async () => false,
    signIn: async () => {},
    signOut: async () => {},
  }),
}));

mock.module("@/lib/atprotoClient", () => ({
  createOAuthAgent: () => ({
    post: postMock,
    like: likeMock,
    deleteLike: mock(async () => undefined),
    repost: mock(async () => undefined),
    deleteRepost: mock(async () => undefined),
  }),
  createPublicAppViewAgent: () => ({
    api: {
      app: {
        bsky: {
          feed: {
            getPosts: getPostsMock,
          },
        },
      },
    },
  }),
}));

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });

  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

const entry: EntryDetail = {
  entryId: "at://did:plc:author/site.standard.document/post1",
  title: "A good article",
  publishedAt: "2026-06-22T00:00:00.000Z",
  contentHtml: "",
  originalUrl: "https://example.com/a-good-article",
};

describe("useEntrySocial", () => {
  beforeEach(() => {
    postMock.mockClear();
    likeMock.mockClear();
    getPostsMock.mockClear();
    getTokenInfoMock.mockClear();
  });

  it("creates a quote post when the OAuth grant includes Bluesky post access", async () => {
    getTokenInfoMock.mockResolvedValueOnce({
      scope:
        "atproto repo:app.bsky.feed.post?action=create&action=delete",
      iss: "https://pds.example",
      aud: "https://pds.example",
      sub: "did:plc:me",
    });

    const { result } = renderHook(() => useEntrySocial(entry), {
      wrapper: makeWrapper(),
    });

    await result.current.quoteMutation.mutateAsync("Worth reading");

    expect(postMock).toHaveBeenCalledTimes(1);
    const postedRecord = postMock.mock.calls[0]?.[0] as unknown;
    expect(postedRecord).toMatchObject({
      text: "Worth reading",
      embed: {
        $type: "app.bsky.embed.external",
        external: {
          uri: "https://example.com/a-good-article",
          title: "A good article",
          associatedRecord:
            "at://did:plc:author/site.standard.document/post1",
        },
      },
    });
  });

  it("fails before writing when the OAuth grant is missing Bluesky post access", async () => {
    getTokenInfoMock.mockResolvedValueOnce({
      scope: "atproto repo:app.thesocialwire.folder?action=create",
      iss: "https://pds.example",
      aud: "https://pds.example",
      sub: "did:plc:me",
    });

    const { result } = renderHook(() => useEntrySocial(entry), {
      wrapper: makeWrapper(),
    });

    await expect(
      result.current.quoteMutation.mutateAsync("Worth reading")
    ).rejects.toThrow("posting permission needs to be refreshed");
    expect(postMock).not.toHaveBeenCalled();
  });

  it("checks like create access before creating a Bluesky like", async () => {
    getTokenInfoMock.mockResolvedValueOnce({
      scope:
        "atproto repo:app.bsky.feed.like?action=create&action=delete",
      iss: "https://pds.example",
      aud: "https://pds.example",
      sub: "did:plc:me",
    });

    const linkedEntry: EntryDetail = {
      ...entry,
      bskyPostUri: "at://did:plc:author/app.bsky.feed.post/root",
      bskyPostCid: "bafyreigood",
    };
    const { result } = renderHook(() => useEntrySocial(linkedEntry), {
      wrapper: makeWrapper(),
    });

    await result.current.toggleLikeMutation.mutateAsync({});

    expect(likeMock).toHaveBeenCalledWith(
      "at://did:plc:author/app.bsky.feed.post/root",
      "bafyreigood"
    );
  });
});
