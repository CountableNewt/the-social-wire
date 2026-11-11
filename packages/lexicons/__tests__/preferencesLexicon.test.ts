import { expect, test } from "bun:test";
import { Lexicons } from "@atproto/lexicon";
import preferences from "../app/thesocialwire/preferences.json";

const lexicons = new Lexicons([preferences as never]);
const record = { $type: "app.thesocialwire.preferences", createdAt: "2026-11-10T00:00:00Z", updatedAt: "2026-11-10T00:00:00Z" };

test("automatic read preference is an optional boolean for compatible existing records", () => {
  expect(() => lexicons.assertValidRecord(record.$type, record)).not.toThrow();
  for (const automaticallyMarkAsRead of [true, false]) expect(() => lexicons.assertValidRecord(record.$type, { ...record, automaticallyMarkAsRead })).not.toThrow();
  expect(() => lexicons.assertValidRecord(record.$type, { ...record, automaticallyMarkAsRead: "false" })).toThrow();
});
