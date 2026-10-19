// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import StackAccessScreen from "./StackAccessScreen";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { GrantView, UserProfile } from "../../api/types";

const ME = "user_me";
const grant = (user_sub: string, display_name: string, role: string, email = `${user_sub}@acme.example`): GrantView => ({ user_sub, display_name, email, role });

const team = (): GrantView[] => [
  grant("mei", "Mei Chen", "viewer"),
  grant(ME, "Sam Park", "owner"),
  grant("lena", "Lena Novak", "approver"),
  grant("priya", "Priya Raman", "owner"),
  grant("alex", "Alex Kim", "operator")
];

interface Sent {
  method: string;
  url: string;
  body?: { role?: string };
}

// A stateful grants API: a PUT or DELETE changes what the next list returns,
// as the server would. `grants: "fail"` fails the list; `"stall"` never answers it.
function serve({
  grants = team(),
  users = [],
  canManageAfter = true
}: { grants?: GrantView[] | "fail" | "stall"; users?: UserProfile[]; canManageAfter?: boolean } = {}) {
  const sent: Sent[] = [];
  let current = Array.isArray(grants) ? grants : [];
  const gone: GrantView[] = [];
  const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json" } });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? (JSON.parse(String(init.body)) as Sent["body"]) : undefined;
    sent.push({ method, url, body });
    const path = new URL(url, "http://localhost").pathname;
    if (path.endsWith("/users/search")) return json({ items: users, next_cursor: null });
    if (path.endsWith("/grants")) {
      if (grants === "stall") return new Promise<Response>(() => {});
      if (grants === "fail") return json({ title: "Internal Server Error", status: 500 }, 500);
      return json({ items: current, next_cursor: null });
    }
    const sub = decodeURIComponent(path.slice(path.lastIndexOf("/") + 1));
    if (path.includes("/grants/") && method === "PUT") {
      const known = current.find((g) => g.user_sub === sub) ?? gone.find((g) => g.user_sub === sub);
      const person = users.find((u) => u.sub === sub);
      const next = { user_sub: sub, display_name: known?.display_name ?? person?.display_name ?? sub, email: known?.email ?? person?.email ?? "", role: body?.role ?? "" };
      current = current.filter((g) => g.user_sub !== sub).concat([next]);
      return json(next);
    }
    if (path.includes("/grants/") && method === "DELETE") {
      gone.push(...current.filter((g) => g.user_sub === sub));
      current = current.filter((g) => g.user_sub !== sub);
      return new Response(null, { status: 204 });
    }
    // The stack itself.
    return json({
      stack: {
        id: "stack_1",
        name: "prod",
        slug: "prod",
        effective_capabilities: { can_view: canManageAfter, can_operate: canManageAfter, can_approve: canManageAfter, can_manage_access: canManageAfter }
      },
      templates: []
    });
  });
  return sent;
}

function auth(): AuthContextValue {
  return {
    me: { sub: ME, tenant_id: "tenant_123", display_name: "Sam Park", global_capabilities: { can_create_stack: true, can_publish_template: true } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function Stacks() {
  const notice = (useLocation().state as { notice?: string } | null)?.notice;
  return <p data-testid="stacks-page">{notice}</p>;
}

function renderScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth()}>
        <MemoryRouter initialEntries={["/stacks/stack_1/access"]}>
          <Routes>
            <Route path="/stacks/:stackId/access" element={<StackAccessScreen />} />
            <Route path="/stacks" element={<Stacks />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

const row = (sub: string) => screen.getByTestId(`grant-${sub}`);
const names = () => screen.getAllByTestId(/^grant-/).map((li) => li.querySelector("span span")?.textContent);
const writes = (sent: Sent[]) => sent.filter(({ method }) => method !== "GET").map(({ method, url, body }) => `${method} ${url.slice(url.lastIndexOf("/") + 1)}${body?.role ? ` ${body.role}` : ""}`);

/** Opens a RoleSelect and picks a role. Base UI opens a select on pointer down. */
async function pickRole(user: ReturnType<typeof userEvent.setup>, trigger: HTMLElement, role: string) {
  await user.click(trigger);
  const option = await screen.findByRole("option", { name: new RegExp(`^${role}`) });
  await user.click(option);
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("StackAccessScreen", () => {
  it("lists people by role, highest first, then by name, and marks you", async () => {
    serve();
    renderScreen();
    await screen.findByTestId("grant-mei");
    expect(names()).toEqual(["Priya Raman", "Sam Park", "Lena Novak", "Alex Kim", "Mei Chen"]);
    // "Name (you)": the marker joins the name (owner's pick, 2026-10-19).
    expect(row(ME).querySelector("span span")?.parentElement?.textContent).toBe("Sam Park (you)");
    expect(screen.getByTestId("page-count").textContent).toBe("5");
    expect(screen.getByRole("heading", { level: 1, name: "Access" })).toBeTruthy();
    // No line explains what the screen already shows (owner, 2026-10-19).
    for (const cut of ["Who can see and change", "Search for someone who has signed in", "Person"]) {
      expect(screen.queryByText(cut, { exact: false })).toBeNull();
    }
  });

  it("waits to add anyone while the list loads", async () => {
    serve({ grants: "stall" });
    renderScreen();
    expect(await screen.findByTestId("access-loading")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Grant access" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("person-search") as HTMLInputElement).disabled).toBe(true);
  });

  it("says a failed list failed, offers Retry, and keeps adding locked", async () => {
    const sent = serve({ grants: "fail" });
    renderScreen();
    expect((await screen.findByRole("alert")).textContent).toContain("Something went wrong while loading who has access to");
    expect((screen.getByRole("button", { name: "Grant access" }) as HTMLButtonElement).disabled).toBe(true);
    const before = sent.filter(({ url }) => url.endsWith("/grants")).length;
    await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(sent.filter(({ url }) => url.endsWith("/grants")).length).toBe(before + 1));
  });

  it("says no one has access yet", async () => {
    serve({ grants: [] });
    renderScreen();
    expect((await screen.findByTestId("access-empty")).textContent).toContain("No one has access to");
  });

  it("locks the only owner's role and remove, without a line explaining it", async () => {
    serve({ grants: [grant(ME, "Sam Park", "owner"), grant("lena", "Lena Novak", "approver")] });
    renderScreen();
    await screen.findByTestId(`grant-${ME}`);
    // The owner cut the reason line (2026-10-19): the lock stays, the line doesn't.
    expect(row(ME).textContent).not.toContain("only owner");
    expect((within(row(ME)).getByRole("button", { name: "Remove your access" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId(`role-${ME}`) as HTMLButtonElement).disabled).toBe(true);
    expect((within(row("lena")).getByRole("button", { name: "Remove Lena Novak’s access" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("changes someone's role from their row at once", async () => {
    const sent = serve();
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-mei");
    await pickRole(user, screen.getByTestId("role-mei"), "operator");
    await waitFor(() => expect(writes(sent)).toEqual(["PUT mei operator"]));
    await waitFor(() => expect(within(row("mei")).getByTestId("role-mei").textContent).toContain("operator"));
  });

  it("confirms a remove in the row, then keeps the row with Undo", async () => {
    const sent = serve();
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-lena");
    await user.click(within(row("lena")).getByRole("button", { name: "Remove Lena Novak’s access" }));
    expect(within(row("lena")).getByText("Remove Lena Novak’s access to prod?")).toBeTruthy();
    expect(writes(sent)).toEqual([]);
    await user.click(within(row("lena")).getByRole("button", { name: "Remove access" }));
    await waitFor(() => expect(within(row("lena")).getByText("access removed")).toBeTruthy());
    expect(writes(sent)).toEqual(["DELETE lena"]);
    expect(screen.getByRole("status").textContent).toContain("Removed Lena Novak’s access to prod.");
    await user.click(within(row("lena")).getByRole("button", { name: "Undo removing Lena Novak’s access" }));
    await waitFor(() => expect(writes(sent)).toEqual(["DELETE lena", "PUT lena approver"]));
    await waitFor(() => expect(within(row("lena")).getByTestId("role-lena").textContent).toContain("approver"));
  });

  it("cancels a remove without sending anything", async () => {
    const sent = serve();
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-lena");
    await user.click(within(row("lena")).getByRole("button", { name: "Remove Lena Novak’s access" }));
    await user.click(within(row("lena")).getByRole("button", { name: "Cancel" }));
    expect(within(row("lena")).getByTestId("role-lena")).toBeTruthy();
    expect(writes(sent)).toEqual([]);
  });

  it("warns before you remove yourself, then sends you to /stacks with why", async () => {
    const sent = serve({ canManageAfter: false });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId(`grant-${ME}`);
    await user.click(within(row(ME)).getByRole("button", { name: "Remove your access" }));
    expect(within(row(ME)).getByText(/you will lose access to prod right away/)).toBeTruthy();
    await user.click(within(row(ME)).getByRole("button", { name: "Remove my access" }));
    expect((await screen.findByTestId("stacks-page")).textContent).toBe("You no longer have access to prod.");
    expect(writes(sent)).toEqual([`DELETE ${ME}`]);
  });

  it("keeps a platform admin who removes themself on the page, with Undo", async () => {
    serve({ canManageAfter: true });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId(`grant-${ME}`);
    await user.click(within(row(ME)).getByRole("button", { name: "Remove your access" }));
    await user.click(within(row(ME)).getByRole("button", { name: "Remove my access" }));
    await waitFor(() => expect(within(row(ME)).getByText("access removed")).toBeTruthy());
    expect(within(row(ME)).getByRole("button", { name: "Undo removing your access" })).toBeTruthy();
  });

  it("asks before you take away your own owner role", async () => {
    const sent = serve();
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId(`grant-${ME}`);
    await pickRole(user, screen.getByTestId(`role-${ME}`), "approver");
    expect(within(row(ME)).getByText("Make yourself approver on prod?")).toBeTruthy();
    expect(writes(sent)).toEqual([]);
    await user.click(within(row(ME)).getByRole("button", { name: "Change my role" }));
    await waitFor(() => expect(writes(sent)).toEqual([`PUT ${ME} approver`]));
  });

  it("finds people, shows who already has a role, and grants the one picked", async () => {
    const users: UserProfile[] = [
      { sub: "lena", display_name: "Lena Novak", email: "lena@acme.example" },
      { sub: "lee", display_name: "Lee Park", email: "lee@acme.example" }
    ];
    const sent = serve({ users });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-mei");
    await user.type(screen.getByTestId("person-search"), "le");
    const lena = await screen.findByRole("option", { name: /Lena Novak/ });
    expect(lena.getAttribute("aria-disabled")).toBe("true");
    expect(lena.textContent).toContain("already approver");
    const search = sent.find(({ url }) => url.includes("/users/search"));
    expect(new URL(search!.url, "http://localhost").searchParams.get("stack_id")).toBe("stack_1");
    await user.click(screen.getByRole("option", { name: /Lee Park/ }));
    expect(screen.getByTestId("person-picked").textContent).toContain("Lee Park");
    await user.click(screen.getByRole("button", { name: "Grant access" }));
    await waitFor(() => expect(writes(sent)).toEqual(["PUT lee viewer"]));
    await screen.findByTestId("grant-lee");
  });

  it("marks you in the picker as it does in the list", async () => {
    serve({ users: [{ sub: ME, display_name: "Sam Park", email: "sam@acme.example" }] });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-mei");
    await user.type(screen.getByTestId("person-search"), "sa");
    const me = await screen.findByRole("option", { name: /Sam Park/ });
    expect(me.textContent).toContain("Sam Park (you)");
    expect(me.getAttribute("aria-disabled")).toBe("true");
  });

  it("names someone openplan has no record of", async () => {
    const sub = "CiQ5ZjNh";
    serve({ grants: [grant(ME, "Sam Park", "owner"), grant(sub, sub, "viewer", "")] });
    renderScreen();
    await screen.findByTestId(`grant-${sub}`);
    expect(within(row(sub)).getByText("Unknown person")).toBeTruthy();
    expect(row(sub).textContent).toContain(sub);
  });

  it("offers a filter once the list is long", async () => {
    const many = Array.from({ length: 11 }, (_, i) => grant(`u${i}`, `Person ${String(i).padStart(2, "0")}`, "viewer"));
    serve({ grants: [grant(ME, "Sam Park", "owner"), ...many] });
    const user = userEvent.setup();
    renderScreen();
    await screen.findByTestId("grant-u0");
    await user.type(screen.getByTestId("access-filter"), "Person 03");
    expect(names()).toEqual(["Person 03"]);
  });
});
