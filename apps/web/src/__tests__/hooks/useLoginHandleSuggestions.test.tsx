import { afterEach, expect, it } from "bun:test";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useLoginHandleSuggestions } from "@/hooks/useLoginHandleSuggestions";

afterEach(cleanup);

it("does not offer a cached previous handle while a new handle or DID is debouncing", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const alice = [{ did: "did:plc:alice", handle: "alice.test" }];
  const bob = [{ did: "did:plc:bob", handle: "bob.test" }];
  client.setQueryData(["loginHandleTypeahead", "alice"], alice);
  client.setQueryData(["loginHandleTypeahead", "bob"], bob);
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  const hook = renderHook(({ value }) => useLoginHandleSuggestions(value), { initialProps: { value: "alice" }, wrapper });
  expect(hook.result.current.data).toEqual(alice);
  hook.rerender({ value: "bob" });
  expect(hook.result.current.data).toBeUndefined();
  await waitFor(() => expect(hook.result.current.data).toEqual(bob));
  hook.rerender({ value: "did:plc:bob" });
  expect(hook.result.current.data).toBeUndefined();
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 250)); });
  expect(hook.result.current.data).toBeUndefined();
  hook.unmount();
  client.clear();
});
