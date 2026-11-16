import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as Auth from "@/hooks/useAuth";
import * as Suggestions from "@/hooks/useLoginHandleSuggestions";
import { PublicationAuthFields } from "@/components/AppSidebar/AddPublication/PublicationAuthFields";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";

const signIn = mock(async (handle: string) => { void handle; });
const restores: (() => void)[] = [];
beforeEach(() => {
  signIn.mockClear();
  const auth = spyOn(Auth, "useAuth").mockReturnValue({ signIn } as unknown as ReturnType<typeof Auth.useAuth>);
  const suggestions = spyOn(Suggestions, "useLoginHandleSuggestions").mockReturnValue({ data: [{ did: "did:plc:author", handle: "author.example", displayName: "Author" }], isFetching: false } as unknown as ReturnType<typeof Suggestions.useLoginHandleSuggestions>);
  restores.push(() => auth.mockRestore(), () => suggestions.mockRestore());
  for (const name of ["HTMLInputElement", "getComputedStyle", "requestAnimationFrame", "cancelAnimationFrame"] as const) {
    const previous = Object.getOwnPropertyDescriptor(globalThis, name);
    const value = name === "getComputedStyle" ? window.getComputedStyle.bind(window) : name === "requestAnimationFrame" ? (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0) : name === "cancelAnimationFrame" ? clearTimeout : window.HTMLInputElement;
    Object.defineProperty(globalThis, name, { configurable: true, value });
    restores.push(() => { if (previous) Object.defineProperty(globalThis, name, previous); else Reflect.deleteProperty(globalThis, name); });
  }
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });

test("selects an actor suggestion before explicit publication authorization", async () => {
  render(<Dialog open><DialogContent><DialogTitle>Authorize Publications</DialogTitle><DialogDescription>Choose an account.</DialogDescription><PublicationAuthFields idPrefix="test" lead={null} seedHandle="" /></DialogContent></Dialog>);
  const input = screen.getByRole("combobox", { name: "Bluesky or ATProto handle" });
  fireEvent.focus(input);
  fireEvent.click(screen.getByRole("option", { name: /Author.*@author.example/ }));
  expect((input as HTMLInputElement).value).toBe("author.example");
  expect(signIn).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Authorize Publication Subscriptions" }));
  await waitFor(() => expect(signIn).toHaveBeenCalledWith("author.example"));
});
