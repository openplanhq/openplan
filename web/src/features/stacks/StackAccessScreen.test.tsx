// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import StackAccessScreen from "./StackAccessScreen";
import type { GrantView, ListGrantsResponse, SearchUsersResponse, UserProfile } from "../../api/types";

function wrapper(initialRoute = "/stacks/stack_123/access") {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false }
    }
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[initialRoute]}>
          <Routes>
            <Route path="/stacks/:stackId/access" element={children} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}

const emptyGrants: ListGrantsResponse = { grants: [] };

const twoGrants: ListGrantsResponse = {
  grants: [
    {
      userSub: "u1",
      role: "owner",
      displayName: "Alice",
      email: "alice@example.com"
    },
    {
      userSub: "u2",
      role: "viewer",
      displayName: "Bob",
      email: "bob@example.com"
    }
  ]
};

const searchResults: SearchUsersResponse = {
  users: [
    {
      sub: "u3",
      displayName: "Charlie Brown",
      email: "charlie@example.com"
    }
  ],
  first: 0,
  max: 20
};

const charlie = searchResults.users[0];
const chad: UserProfile = { sub: "u4", displayName: "Chad", email: "chad@example.com" };
// Matched by email: there is no "ch" in her name.
const dorothy: UserProfile = { sub: "u5", displayName: "Dorothy Vaughan", email: "dvaughan@chem.example.com" };

interface Sent {
  method: string;
  url: string;
  body?: Record<string, string>;
}

// Answers each request by its URL, so a test can count and inspect what the
// screen sent. A search gets `users`, after `searchDelay` ms; a POST gets
// `assign` (or the grant it asked for); anything else gets the grants list.
function serve({
  grants = [],
  users = [],
  assign,
  searchDelay = 0
}: { grants?: GrantView[]; users?: UserProfile[]; assign?: () => Promise<Response>; searchDelay?: number } = {}) {
  const sent: Sent[] = [];
  const json = (value: unknown) =>
    new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, string>) : undefined;
    sent.push({ method, url, body });
    if (url.includes("/users/search")) {
      await new Promise((resolve) => setTimeout(resolve, searchDelay));
      return json({ users, first: 0, max: 20 });
    }
    if (method === "POST") {
      return assign ? assign() : json({ userSub: body?.user_sub, role: body?.role, displayName: "", email: "" });
    }
    return json({ grants });
  });
  return sent;
}

/** The query of each user search the screen sent, in order. */
const searches = (sent: Sent[]) =>
  sent
    .filter(({ url }) => url.includes("/users/search"))
    .map(({ url }) => new URL(url, "http://localhost").searchParams.get("q"));

/** The search waits 300ms after the last keystroke. */
const afterDebounce = () => new Promise((resolve) => setTimeout(resolve, 400));

async function renderWithGrants(grants: GrantView[] = []) {
  render(<StackAccessScreen />, { wrapper: wrapper() });
  await screen.findByText(grants.length === 0 ? /No users have been assigned/ : grants[0].displayName);
}

async function typeInSearch(text: string) {
  const user = userEvent.setup();
  const input = screen.getByRole("combobox", { name: "Search users" }) as HTMLInputElement;
  await user.click(input);
  await user.type(input, text);
  return { user, input };
}

// Only while no popup is open: Base UI hides the rest of the page from
// assistive technology while one is, and a role query can't name it then.
const assignButton = () => screen.getByRole("button", { name: "Assign Role" }) as HTMLButtonElement;

describe("StackAccessScreen", () => {
  beforeEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows loading state for grants", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      () => new Promise(() => {})
    );

    render(<StackAccessScreen />, { wrapper: wrapper() });

    expect(screen.getByText("Loading grants...")).toBeDefined();
  });

  it("renders grants list when data is loaded", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(JSON.stringify(twoGrants), {
        status: 200,
        headers: { "content-type": "application/json" }
      })
    );

    render(<StackAccessScreen />, { wrapper: wrapper() });

    await waitFor(() => {
      expect(screen.getByText("Alice")).toBeDefined();
    });
    expect(screen.getByText("Bob")).toBeDefined();
    expect(screen.getByText("owner")).toBeDefined();
    expect(screen.getByText("viewer")).toBeDefined();
  });

  it("shows empty state when no grants", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(JSON.stringify(emptyGrants), {
        status: 200,
        headers: { "content-type": "application/json" }
      })
    );

    render(<StackAccessScreen />, { wrapper: wrapper() });

    await waitFor(() => {
      expect(
        screen.getByText(
          "No users have been assigned access yet. Use the panel on the right to add the first grant."
        )
      ).toBeDefined();
    });
  });

  it("shows search results when typing 2+ characters", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify(emptyGrants), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(searchResults), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      );

    render(<StackAccessScreen />, { wrapper: wrapper() });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getByText(/No users/)).toBeDefined();
    });

    const searchInput = screen.getByLabelText("Search users");
    await user.click(searchInput);
    await user.type(searchInput, "cha");

    await waitFor(() => {
      expect(screen.getByText("Charlie Brown")).toBeDefined();
    });
  });

  it("shows confirm state and triggers revoke", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify(twoGrants), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      )
      .mockResolvedValueOnce(
        new Response(null, {
          status: 204
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ grants: [twoGrants.grants[0]] }), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      );

    render(<StackAccessScreen />, { wrapper: wrapper() });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getByText("Bob")).toBeDefined();
    });

    const revokeButton = screen.getByLabelText("Revoke Bob's viewer role");
    await user.click(revokeButton);

    expect(screen.getByText("Remove access?")).toBeDefined();
    expect(screen.getByText("Confirm")).toBeDefined();

    await user.click(screen.getByText("Confirm"));

    await waitFor(() => {
      expect(screen.getByText(/Removed Bob.*viewer access/)).toBeDefined();
    });
  });

  it("shows error banner on failed revoke", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify(twoGrants), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: "last_owner",
            message: "cannot remove the last owner"
          }),
          {
            status: 409,
            headers: { "content-type": "application/json" }
          }
        )
      );

    render(<StackAccessScreen />, { wrapper: wrapper() });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getByText("Alice")).toBeDefined();
    });

    await user.click(screen.getByLabelText("Revoke Alice's owner role"));
    await user.click(screen.getByText("Confirm"));

    await waitFor(() => {
      expect(
        screen.getByText("cannot remove the last owner")
      ).toBeDefined();
    });
    expect(screen.getByText("cannot remove the last owner").closest('[data-slot="alert"]')).not.toBeNull();
  });

  it("assigns role when selecting a user and clicking assign", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(JSON.stringify(emptyGrants), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(searchResults), {
          status: 200,
          headers: { "content-type": "application/json" }
        })
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            userSub: "u3",
            role: "viewer",
            displayName: "Charlie Brown"
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" }
          }
        )
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            grants: [
              {
                userSub: "u3",
                role: "viewer",
                displayName: "Charlie Brown",
                email: "charlie@example.com"
              }
            ]
          }),
          {
            status: 200,
            headers: { "content-type": "application/json" }
          }
        )
      );

    render(<StackAccessScreen />, { wrapper: wrapper() });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getByText(/No users/)).toBeDefined();
    });

    await user.click(screen.getByLabelText("Search users"));
    await user.type(screen.getByLabelText("Search users"), "cha");

    await waitFor(() => {
      expect(screen.getByText("Charlie Brown")).toBeDefined();
    });

    await user.click(screen.getByText("Charlie Brown"));
    await user.click(screen.getByRole("button", { name: "Assign Role" }));

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledTimes(4);
    });
  });

  it("has proper tab order for keyboard accessibility", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(emptyGrants), {
        status: 200,
        headers: { "content-type": "application/json" }
      })
    );

    render(<StackAccessScreen />, { wrapper: wrapper() });
    const user = userEvent.setup();

    await waitFor(() => {
      expect(screen.getByText(/No users/)).toBeDefined();
    });

    await user.tab();
    expect(screen.getByLabelText("Search users")).toEqual(
      document.activeElement
    );

    await user.tab();
    expect(screen.getByLabelText("Role")).toEqual(document.activeElement);

    const assignButton = screen.getByRole("button", { name: "Assign Role" });
    expect((assignButton as HTMLButtonElement).disabled).toBe(true);
  });
  describe("user search", () => {
    it("is a combobox that stays shut until a search of two characters answers", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("c");
      await afterDebounce();
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(screen.queryByText("No users found")).toBeNull();
      expect(searches(sent)).toEqual([]);

      await user.type(input, "h");
      await screen.findByRole("option", { name: /Charlie Brown/ });
      expect(input.getAttribute("aria-expanded")).toBe("true");
      expect(searches(sent)).toEqual(["ch"]);
    });

    // The server matches on name and email; the list must not filter again.
    it("lists every user the server answers with, even one matched by email", async () => {
      serve({ users: [charlie, dorothy] });
      await renderWithGrants();

      await typeInSearch("ch");
      expect(await screen.findByRole("option", { name: /Dorothy Vaughan/ })).toBeDefined();
      expect(screen.getAllByRole("option")).toHaveLength(2);
    });

    it("moves the highlight with ArrowDown and ArrowUp, and picks with Enter", async () => {
      serve({ users: [charlie, chad] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("ch");
      const [first, second] = await screen.findAllByRole("option");
      await user.keyboard("{ArrowDown}");
      expect(input.getAttribute("aria-activedescendant")).toBe(first.id);
      await user.keyboard("{ArrowDown}");
      expect(input.getAttribute("aria-activedescendant")).toBe(second.id);
      expect(second.hasAttribute("data-highlighted")).toBe(true);
      await user.keyboard("{ArrowUp}");
      expect(input.getAttribute("aria-activedescendant")).toBe(first.id);

      await user.keyboard("{Enter}");
      expect(input.value).toBe("Charlie Brown");
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(assignButton().disabled).toBe(false);
    });

    it("closes and clears on Escape", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await screen.findByRole("option", { name: /Charlie Brown/ });
      await user.keyboard("{Escape}");
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(screen.queryByRole("option")).toBeNull();
      expect(input.value).toBe("");
    });

    it("keeps the pick, and searches only what was typed, while the user looks again", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      expect(input.value).toBe("Charlie Brown");
      await afterDebounce();
      expect(searches(sent)).toEqual(["cha"]);

      // Typing searches again; leaving without a new pick puts the pick back.
      await user.type(input, "x");
      await user.tab();
      expect(input.value).toBe("Charlie Brown");
      expect(assignButton().disabled).toBe(false);
    });

    // Base UI resets its input to the pick, or empties it, whenever the list
    // closes. The list closes on its own while a new search loads or once the
    // query is too short, and neither may cost the user what they typed.
    it("keeps what was typed while a refined search loads", async () => {
      serve({ users: [charlie, chad], searchDelay: 200 });
      await renderWithGrants();

      const { user, input } = await typeInSearch("ch");
      await screen.findAllByRole("option");
      await user.type(input, "a");
      await afterDebounce();
      expect(input.value).toBe("cha");
      expect(screen.getAllByRole("option")).toHaveLength(2);
    });

    it("keeps a query shortened below two characters", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("ch");
      await screen.findAllByRole("option");
      await user.keyboard("{Backspace}");
      await afterDebounce();
      expect(input.value).toBe("c");
      expect(input.getAttribute("aria-expanded")).toBe("false");
    });

    it("keeps what was typed after a pick while that search runs", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      await user.type(input, "x");
      await afterDebounce();
      expect(input.value).toBe("Charlie Brownx");
    });

    it("sends no search for what was typed once the user leaves", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      await user.type(input, "x");
      await user.tab();
      await afterDebounce();
      expect(input.value).toBe("Charlie Brown");
      expect(searches(sent)).toEqual(["cha"]);
    });

    it("puts the pick back when the user leaves a query too short to search", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      await user.type(input, "d", { initialSelectionStart: 0, initialSelectionEnd: input.value.length });
      await afterDebounce();
      expect(input.value).toBe("d");
      await user.tab();
      expect(input.value).toBe("Charlie Brown");
      expect(assignButton().disabled).toBe(false);
    });

    it("clears the pick with the clear button", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      await user.click(screen.getByRole("button", { name: "Clear selected user" }));
      expect(input.value).toBe("");
      expect(screen.queryByRole("button", { name: "Clear selected user" })).toBeNull();
      expect(assignButton().disabled).toBe(true);
    });

    it("shows a user who already holds a role with it, and won't pick them", async () => {
      const alice = twoGrants.grants[0];
      serve({ grants: [alice], users: [{ sub: alice.userSub, displayName: alice.displayName, email: alice.email }] });
      await renderWithGrants([alice]);

      const { user, input } = await typeInSearch("ali");
      const option = await screen.findByRole("option", { name: /Alice/ });
      expect(option.getAttribute("aria-disabled")).toBe("true");
      expect(option.textContent).toContain("alice@example.com — owner");

      await user.keyboard("{ArrowDown}{Enter}");
      expect(input.value).toBe("ali");
      // jsdom has no CSS, so the click reaches the option that pointer-events-none
      // shields in a browser. Either way nothing is picked.
      await user.click(option);
      await user.keyboard("{Escape}");
      expect(input.value).toBe("");
      expect(assignButton().disabled).toBe(true);
    });

    it("says No users found when the search finds nobody", async () => {
      serve({ users: [] });
      await renderWithGrants();

      await typeInSearch("zz");
      expect(await screen.findByText("No users found")).toBeDefined();
    });
  });

  describe("role picker", () => {
    it("opens from the keyboard, moves with the arrows, and commits with Enter", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();
      const { user } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));

      const role = screen.getByRole("combobox", { name: "Role" });
      const shown = () => role.querySelector('[data-slot="select-value"]')?.textContent;
      expect(shown()).toBe("Viewer");

      role.focus();
      await user.keyboard("{Enter}");
      const listbox = await screen.findByRole("listbox");
      expect(within(listbox).getAllByRole("option").map((option) => option.textContent)).toEqual([
        "Owner",
        "Operator",
        "Approver",
        "Viewer"
      ]);
      expect(document.activeElement?.textContent).toBe("Viewer");
      await user.keyboard("{ArrowUp}{ArrowUp}");
      expect(document.activeElement?.textContent).toBe("Operator");
      await user.keyboard("{ArrowDown}");
      expect(document.activeElement?.textContent).toBe("Approver");
      await user.keyboard("{Enter}");
      await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
      expect(shown()).toBe("Approver");
      expect(document.activeElement).toBe(role);

      await user.click(assignButton());
      await waitFor(() =>
        expect(sent.find(({ method }) => method === "POST")?.body).toEqual({ user_sub: "u3", role: "approver" })
      );
    });
  });

  it("sends one assignment however often Assign is pressed while it is out", async () => {
    const sent = serve({ users: [charlie], assign: () => new Promise<Response>(() => {}) });
    await renderWithGrants();
    const { user } = await typeInSearch("cha");
    await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));

    const assign = assignButton();
    await user.click(assign);
    expect(assign.textContent).toBe("Assigning...");
    expect(assign.disabled).toBe(true);
    await user.click(assign);
    expect(sent.filter(({ method }) => method === "POST")).toHaveLength(1);
  });
  describe("current grants", () => {
    it("shows each grant's role as a RoleBadge", async () => {
      serve({ grants: twoGrants.grants });
      await renderWithGrants(twoGrants.grants);

      const owner = screen.getByText("owner");
      expect(owner.getAttribute("data-slot")).toBe("badge");
      expect(owner.getAttribute("data-role")).toBe("owner");
    });

    it("cuts a long name or email short and shows it whole on hover", async () => {
      const name = "Bartholomew Montgomery-Fitzwilliam the Third, Platform Operations";
      const email = "bartholomew.montgomery-fitzwilliam@platform-operations.example.com";
      serve({ grants: [{ userSub: "u9", role: "viewer", displayName: name, email }] });
      render(<StackAccessScreen />, { wrapper: wrapper() });

      for (const text of [name, email]) {
        const element = await screen.findByText(text);
        expect(element.classList).toContain("truncate");
        expect(element.getAttribute("title")).toBe(text);
      }
    });

    it("puts a failed load in an Alert, with a Retry that loads again", async () => {
      let calls = 0;
      vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
        calls += 1;
        return calls === 1
          ? new Response(JSON.stringify({ error: "internal", message: "boom" }), {
              status: 500,
              headers: { "content-type": "application/json" }
            })
          : new Response(JSON.stringify(twoGrants), { status: 200, headers: { "content-type": "application/json" } });
      });
      render(<StackAccessScreen />, { wrapper: wrapper() });

      expect((await screen.findByRole("alert")).textContent).toContain("Failed to load grants.");
      await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));
      expect(await screen.findByText("Alice")).toBeDefined();
    });

    it("offers Undo in a status banner after a revoke, and Undo restores the role", async () => {
      const sent = serve({ grants: twoGrants.grants });
      await renderWithGrants(twoGrants.grants);
      const user = userEvent.setup();

      await user.click(screen.getByRole("button", { name: "Revoke Bob's viewer role" }));
      await user.click(screen.getByRole("button", { name: "Confirm" }));
      const banner = (await screen.findByText(/Removed Bob.*viewer access/)).closest<HTMLElement>('[role="status"]');
      expect(banner).not.toBeNull();

      await user.click(within(banner as HTMLElement).getByRole("button", { name: "Undo" }));
      await waitFor(() =>
        expect(sent.find(({ method }) => method === "POST")?.body).toEqual({ user_sub: "u2", role: "viewer" })
      );
    });
  });
});
