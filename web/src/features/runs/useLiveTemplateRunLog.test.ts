// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FakeEventSource } from "./eventSourceTestSupport";
import { useLiveTemplateRunLog } from "./useLiveTemplateRunLog";

const URL = "/v1/tenants/tenant_123/template-runs/run_1/logs/plan-3/events";

function onlySource(): FakeEventSource {
  expect(FakeEventSource.instances).toHaveLength(1);
  return FakeEventSource.instances[0];
}

describe("useLiveTemplateRunLog", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The server sends nothing a client already holds, across reconnects, so
  // the client appends what it is sent and counts nothing.
  it("opens the log's stream and appends its pieces in the order they arrive", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();
    expect(source.url).toBe(URL);
    expect(result.current).toEqual({ pieces: [], streamedBytes: 0, gap: false, truncated: false, ended: null, failed: false });

    act(() => source.dispatch("log", { offset: 0, end: 16, data: "Initializing...\n" }, "1-0"));
    act(() => source.dispatch("log", { offset: 16, end: 22, data: "Done.\n" }, "2-0"));

    expect(result.current.pieces).toEqual(["Initializing...\n", "Done.\n"]);
    expect(result.current.ended).toBeNull();
  });

  // The server counts the stored log's raw bytes, which replaced characters
  // in the text would miscount, so its end is kept as sent.
  it("keeps the byte count the server gave the last piece", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 0, end: 5, data: "\uFFFDok\n" }, "1-0"));
    expect(result.current.streamedBytes).toBe(5);
    act(() => source.dispatch("log", { offset: 5, end: 9, data: "more" }, "2-0"));
    expect(result.current.streamedBytes).toBe(9);
    // A piece with no end leaves the count where it was.
    act(() => source.dispatch("log", { offset: 9, data: "x" }, "3-0"));
    expect(result.current.streamedBytes).toBe(9);
  });

  it("skips an event whose data is not JSON, and keeps following", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 0, end: 4, data: "one\n" }, "1-0"));
    act(() => source.dispatchRaw("log", "{not json"));
    act(() => source.dispatchRaw("log_end", "{not json"));
    act(() => source.dispatch("log", { offset: 4, end: 8, data: "two\n" }, "2-0"));

    expect(result.current.pieces).toEqual(["one\n", "two\n"]);
    expect(result.current.ended).toBeNull();
    expect(source.closeCalls).toBe(0);
  });

  // A state a newer server might send is not a reason to stop following.
  it("reads a log_end state it does not know as still live", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log_end", { state: "archived" }));

    expect(result.current.ended).toBeNull();
    expect(source.closeCalls).toBe(0);
  });

  it("ends, and closes the stream, on log_end", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 0, end: 4, data: "out\n" }, "1-0"));
    act(() => source.dispatch("log_end", { state: "complete" }));

    expect(result.current.ended).toBe("complete");
    expect(result.current.pieces).toEqual(["out\n"]);
    expect(source.closeCalls).toBe(1);
  });

  it("ends a lost log the same way", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log_end", { state: "lost" }));

    expect(result.current.ended).toBe("lost");
    expect(source.closeCalls).toBe(1);
  });

  // The server dropped this client while the command still runs; the browser
  // reconnects on its own, so the stream stays open.
  it("keeps following after a log_end that says the log is still live", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log_end", { state: "live" }));

    expect(result.current.ended).toBeNull();
    expect(source.closeCalls).toBe(0);
  });

  // Valkey keeps nothing on disk: after it restarts mid-command the stream
  // starts again from wherever the executor had got to, and a piece arrives
  // past the end of the last one. What lies between never streamed.
  it("marks a gap when a piece starts past what streamed so far", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 0, end: 4, data: "one\n" }, "1-0"));
    expect(result.current.gap).toBe(false);
    act(() => source.dispatch("log", { offset: 40, end: 44, data: "ten\n" }, "2-0"));

    expect(result.current.gap).toBe(true);
    expect(result.current.pieces).toEqual(["one\n", "ten\n"]);
    expect(result.current.streamedBytes).toBe(44);
  });

  // A fresh viewer of a stream that restarted gets its first piece from the
  // middle of the log, which is a gap at the start.
  it("marks a gap when the first piece does not start at the log's first byte", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 40, end: 44, data: "ten\n" }, "1-0"));

    expect(result.current.gap).toBe(true);
  });

  it("marks the log truncated on log_truncated", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.dispatch("log", { offset: 0, end: 6, data: "a lot\n" }, "1-0"));
    act(() => source.dispatch("log_truncated", {}));

    expect(result.current.truncated).toBe(true);
    expect(result.current.pieces).toEqual(["a lot\n"]);
    expect(result.current.ended).toBeNull();
  });

  it("fails only when the browser has given up reconnecting", () => {
    const { result } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    act(() => source.fail(FakeEventSource.CONNECTING));
    expect(result.current.failed).toBe(false);

    act(() => source.fail(FakeEventSource.CLOSED));
    expect(result.current.failed).toBe(true);
  });

  it("closes the stream on unmount", () => {
    const { unmount } = renderHook(() => useLiveTemplateRunLog(URL));
    const source = onlySource();

    unmount();

    expect(source.closeCalls).toBe(1);
  });

  // The state is one stream's: a caller that can switch logs keys its
  // component by URL, so the hook only has to move to the new stream.
  it("closes the old stream and opens the new one when the URL changes", () => {
    const { rerender } = renderHook(({ url }: { url: string }) => useLiveTemplateRunLog(url), { initialProps: { url: URL } });
    const first = onlySource();

    const next = URL.replace("plan-3", "apply-3");
    rerender({ url: next });

    expect(first.closeCalls).toBe(1);
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.instances[1].url).toBe(next);
  });
});
