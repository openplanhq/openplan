// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import PlatformAccessScreen from "./PlatformAccessScreen";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { GrantView, UserProfile } from "../../api/types";

const ME = "user_me";
const grant = (user_sub: string, display_name: string, role: string): GrantView => ({ user_sub, display_name, email: `${user_sub}@acme.example`, role });
const team = (): GrantView[] => [grant("tom", "Tom Becker", "viewer"), grant(ME, "Sam Park", "admin"), grant("lena", "Lena Novak", "editor")];

interface Sent {
  method: string;
  url: string;
  body?: { role?: string };
}

// A stateful platform grants API, as the server would answer.
function serve({ grants = team(), users = [] }: { grants?: GrantView[]; users?: UserProfile[] } = {}) {
  const sent: Sent[] = [];
  let current = grants;
  const json = (value: unknown) => new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? (JSON.parse(String(init.body)) as Sent["body"]) : undefined;
    sent.push({ method, url, body });
    const path = new URL(url, "http://localhost").pathname;
    if (path.endsWith("/users/search")) return json({ items: users, next_cursor: null });
    if (path.endsWith("/platform/grants")) return json({ items: current, next_cursor: null });
    const sub = decodeURIComponent(path.slice(path.lastIndexOf("/") + 1));
    if (method === "PUT") {
      const person = users.find((u) => u.sub === sub);
      const next = { user_sub: sub, display_name: person?.display_name ?? sub, email: person?.email ?? "", role: body?.role ?? "" };
      current = current.filter((g) => g.user_sub !== sub).concat([next]);
      return json(next);
    }
    current = current.filter((g) => g.user_sub !== sub);
    return new Response(null, { status: 204 });
  });
  return sent;
}

function renderScreen(me = { sub: ME, display_name: "Sam Park", email: "sam@acme.example" }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const auth: AuthContextValue = {
    me: { ...me, tenant_id: "tenant_123", global_capabilities: { can_create_stack: true, can_publish_template: true, can_manage_platform_access: true } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={["/access"]}>
          <PlatformAccessScreen />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

const row = (sub: string) => screen.getByTestId(`grant-${sub}`);
const names = () => screen.getAllByTestId(/^grant-/).map((li) => li.querySelector("span span")?.textContent);
const writes = (sent: Sent[]) => sent.filter(({ method }) => method !== "GET").map(({ method, url, body }) => `${method} ${url.slice(url.lastIndexOf("/") + 1)}${body?.role ? ` ${body.role}` : ""}`);

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("PlatformAccessScreen", () => {
  it("lists root first, locked, then admins, editors and viewers", async () => {
    serve();
    renderScreen();
    await screen.findByTestId("grant-tom");
    expect(names()).toEqual(["Root", "Sam Park", "Lena Novak", "Tom Becker"]);
    expect(screen.queryByTestId("page-count")).toBeNull();
    expect(within(row("root")).queryAllByRole("button")).toEqual([]);
    expect(row("root").textContent).toContain("root");
    // The owner cut the line naming where root comes from (2026-10-19).
    expect(screen.queryByText(/OPENPLAN_ROOT_SUBJECT/)).toBeNull();
  });

  it("shows root its own name on a fresh install, and nothing else in the list", async () => {
    serve({ grants: [] });
    renderScreen({ sub: "root_sub", display_name: "openplan admin", email: "admin@openplan.local" });
    await screen.findByTestId("grant-root");
    expect(row("root").textContent).toBe("openplan admin (you)admin@openplan.localroot");
    expect(screen.queryByTestId("access-empty")).toBeNull();
  });

  it("removes an admin at once, the last one included, with nothing asked", async () => {
    const sent = serve({ grants: [grant("priya", "Priya Raman", "admin"), grant("tom", "Tom Becker", "viewer")] });
    const user = userEvent.setup();
    renderScreen({ sub: "root_sub", display_name: "openplan admin", email: "" });
    await screen.findByTestId("grant-priya");
    await user.click(within(row("priya")).getByRole("button", { name: "Remove Priya Raman’s access" }));
    await waitFor(() => expect(screen.queryByTestId("grant-priya")).toBeNull());
    expect(writes(sent)).toEqual(["DELETE priya"]);
  });

  it("leaves root out while a filter narrows the list", async () => {
    const many = Array.from({ length: 11 }, (_, i) => grant(`u${i}`, `Person ${String(i).padStart(2, "0")}`, "viewer"));
    serve({ grants: [grant(ME, "Sam Park", "admin"), ...many] });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-u0");
    await user.type(screen.getByTestId("access-filter"), "Person 03");
    expect(names()).toEqual(["Person 03"]);
  });

  it("searches the platform, not a stack, and grants the role picked", async () => {
    const users: UserProfile[] = [{ sub: "noor", display_name: "Noor Hassan", email: "noor@acme.example" }];
    const sent = serve({ users });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-tom");
    await user.type(screen.getByTestId("person-search"), "no");
    await user.click(await screen.findByRole("option", { name: /Noor Hassan/ }));
    await user.click(screen.getByRole("button", { name: "Grant access" }));
    await waitFor(() => expect(writes(sent)).toEqual(["PUT noor viewer"]));
    const search = sent.find(({ url }) => url.includes("/users/search"));
    expect(new URL(search!.url, "http://localhost").searchParams.has("stack_id")).toBe(false);
    await screen.findByTestId("grant-noor");
  });
});
