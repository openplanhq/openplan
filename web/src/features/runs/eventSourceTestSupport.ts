// A stand-in for the browser's EventSource, which jsdom lacks. Install it with
// vi.stubGlobal("EventSource", FakeEventSource); each source it opens is kept
// in FakeEventSource.instances so a test can find it and drive it.
export class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static instances: FakeEventSource[] = [];

  readonly url: string;
  readyState = FakeEventSource.CONNECTING;
  closeCalls = 0;
  private readonly listeners = new Map<string, Set<(event: Event) => void>>();

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: Event) => void) {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: (event: Event) => void) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.closeCalls += 1;
    this.readyState = FakeEventSource.CLOSED;
  }

  // Sends one server event, as the API frames it: a JSON data line and, for
  // a log event, the stream id the browser would resend as Last-Event-ID.
  dispatch(type: string, data: unknown, lastEventId = "") {
    this.readyState = FakeEventSource.OPEN;
    this.emit(new MessageEvent(type, { data: JSON.stringify(data), lastEventId }));
  }

  // Sends one server event whose data line is exactly data, which need not
  // be JSON.
  dispatchRaw(type: string, data: string) {
    this.readyState = FakeEventSource.OPEN;
    this.emit(new MessageEvent(type, { data }));
  }

  // Reports a connection error. The browser leaves the source CONNECTING
  // when it will retry, and CLOSED when it has given up.
  fail(readyState: number) {
    this.readyState = readyState;
    this.emit(new Event("error"));
  }

  private emit(event: Event) {
    for (const listener of this.listeners.get(event.type) ?? []) {
      listener(event);
    }
  }
}
