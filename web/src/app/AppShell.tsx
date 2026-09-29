import { Link, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { tenantID } from "../config";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const navItems: { to: string; label: string }[] = [
  { to: "/stacks", label: "Stacks" },
  { to: "/templates", label: "Templates" }
];

const isDebug = import.meta.env.DEV || import.meta.env.VITE_DEBUG === "true";

// Until PR 9, base.css colours and underlines every bare <a> from the legacy
// layer, and a rule on the element beats an inherited colour, so each link
// sets its own.
const navLinkClass = cn(
  buttonVariants({ variant: "ghost", size: "sm" }),
  "text-muted-foreground hover:no-underline pointer-coarse:h-11"
);

export default function AppShell() {
  const { me, logout, status } = useAuth();
  const debugRows: [label: string, testId: string, value: string][] = [
    ["Auth status", "debug-auth-status", status],
    ["User sub", "debug-user-sub", me?.sub ?? "-"],
    ["Display name", "debug-display-name", me?.displayName ?? "-"],
    ["Tenant", "debug-tenant", tenantID],
    ["isPlatformAdmin", "debug-is-platform-admin", me?.globalCapabilities.isPlatformAdmin.toString() ?? "-"],
    ["canCreateStack", "debug-can-create-stack", me?.globalCapabilities.canCreateStack.toString() ?? "-"],
    ["canPublishTemplate", "debug-can-publish-template", me?.globalCapabilities.canPublishTemplate.toString() ?? "-"]
  ];

  return (
    <div className="min-h-screen">
      <a
        href="#main-content"
        className="fixed top-2 left-2 z-50 -translate-y-16 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-transform hover:no-underline focus:translate-y-0"
      >
        Skip to content
      </a>
      {/* z-5 keeps the legacy overlays that must cover this bar above it:
          .search-dropdown (10) and .undo-banner (20) in features.css.
          AppShell.test.tsx checks the order. */}
      <header className="sticky top-0 z-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-3 border-b bg-background px-4 py-3 md:px-6">
        <div className="flex items-center gap-4 md:gap-8">
          <span className="text-lg leading-none font-semibold tracking-tight">openplan</span>
          <nav className="flex items-center gap-1" aria-label="Primary">
            {navItems.map((item) => (
              <Link key={item.to} to={item.to} className={navLinkClass}>
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3 text-sm">
          <div className="flex items-center gap-3 text-muted-foreground" data-testid="identity-menu">
            {status === "loading" && <span data-testid="identity-loading">Loading...</span>}
            {me && (
              <>
                <span data-testid="identity-display-name">{me.displayName}</span>
                <Button
                  variant="outline"
                  size="sm"
                  className="pointer-coarse:h-11"
                  data-testid="logout-button"
                  onClick={logout}
                >
                  Log out
                </Button>
              </>
            )}
          </div>
          <div className="flex min-w-0 items-center gap-2 text-muted-foreground">
            <span>Tenant</span>
            <span
              className="min-w-0 rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-foreground wrap-anywhere"
              data-testid="shell-tenant-context"
            >
              {tenantID}
            </span>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-7xl px-4 py-6" id="main-content" tabIndex={-1}>
        <Outlet />
      </main>
      {isDebug && (
        <details className="mx-4 my-8 border-t pt-4 font-mono text-xs md:mx-6" data-testid="debug-panel">
          <summary className="cursor-pointer tracking-wide text-muted-foreground uppercase">IDs (debug)</summary>
          <dl className="mt-4 grid gap-2">
            {debugRows.map(([label, testId, value]) => (
              <div key={testId} className="flex gap-4">
                <dt className="w-36 shrink-0 text-muted-foreground">{label}</dt>
                <dd className="min-w-0 wrap-anywhere" data-testid={testId}>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        </details>
      )}
    </div>
  );
}
