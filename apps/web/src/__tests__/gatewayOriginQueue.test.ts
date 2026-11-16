import { expect, it } from "bun:test";
import { enqueueForGatewayOrigin } from "@/lib/gatewayOriginQueue";

const deferred = () => {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
};

it("serializes tasks for one origin and runs other origins independently", async () => {
  const order: string[] = [];
  const first = deferred();
  const a = enqueueForGatewayOrigin("https://serial.test", async () => { order.push("a:start"); await first.promise; order.push("a:end"); });
  const b = enqueueForGatewayOrigin("https://serial.test", async () => { order.push("b"); });
  const other = enqueueForGatewayOrigin("https://other.test", async () => { order.push("other"); });
  await other;
  expect(order).toEqual(["a:start", "other"]);
  first.resolve();
  await Promise.all([a, b]);
  expect(order).toEqual(["a:start", "other", "a:end", "b"]);
});

it("releases the origin when a running task exceeds the hold window", async () => {
  const slow = deferred();
  const order: string[] = [];
  const a = enqueueForGatewayOrigin("https://hold.test", async () => { await slow.promise; order.push("slow"); }, { holdMs: 10 });
  await enqueueForGatewayOrigin("https://hold.test", async () => { order.push("fast"); }, { holdMs: 10 });
  expect(order).toEqual(["fast"]);
  slow.resolve();
  await a;
  expect(order).toEqual(["fast", "slow"]);
});

it("skips tasks whose caller aborted while queued without blocking later tasks", async () => {
  const gate = deferred();
  const controller = new AbortController();
  let ranAborted = false;
  const first = enqueueForGatewayOrigin("https://abort.test", () => gate.promise);
  const aborted = enqueueForGatewayOrigin("https://abort.test", async () => { ranAborted = true; }, { signal: controller.signal });
  const later = enqueueForGatewayOrigin("https://abort.test", async () => "later");
  controller.abort(new Error("Caller Timed Out"));
  gate.resolve();
  await first;
  await expect(aborted).rejects.toThrow("Caller Timed Out");
  expect(await later).toBe("later");
  expect(ranAborted).toBe(false);
});

it("keeps the queue moving after a task fails", async () => {
  const failed = enqueueForGatewayOrigin("https://fail.test", async () => { throw new Error("boom"); });
  await expect(failed).rejects.toThrow("boom");
  expect(await enqueueForGatewayOrigin("https://fail.test", async () => 1)).toBe(1);
});
