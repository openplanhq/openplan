import { createBrowserRouter, createMemoryRouter, Navigate, redirect } from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import AppShell from "./AppShell";
import type { RouteHandle } from "./AppShell";
import NotFound from "./NotFound";
import RequireCapability from "../auth/RequireCapability";
import SessionProvider from "../auth/SessionProvider";
import SignInScreen from "../auth/SignInScreen";
import StacksListScreen from "../features/stacks/StacksListScreen";
import StackAttentionScreen from "../features/stacks/StackAttentionScreen";
import StackPage from "../features/stacks/StackPage";
import StackIndexPanel from "../features/stacks/StackIndexPanel";
import AddStackTemplateScreen from "../features/stacks/AddStackTemplateScreen";
import StackSectionLayout from "../features/stacks/StackSectionLayout";
import TemplatePanel from "../features/stacks/TemplatePanel";
import TemplateRunsTab from "../features/stacks/TemplateRunsTab";
import TemplateVariablesTab from "../features/stacks/TemplateVariablesTab";
import TemplateCredentialsTab from "../features/stacks/TemplateCredentialsTab";
import TemplateSettingsTab from "../features/stacks/TemplateSettingsTab";
import UpgradeStackTemplateScreen from "../features/stacks/UpgradeStackTemplateScreen";
import EnvironmentScreen from "../features/stacks/EnvironmentScreen";
import TemplateRegistryScreen from "../features/templates/TemplateRegistryScreen";
import TemplateRegistrationScreen from "../features/templates/TemplateRegistrationScreen";
import TemplateDetailScreen from "../features/templates/TemplateDetailScreen";
import RunDetailScreen from "../features/runs/RunDetailScreen";
import StackAccessScreen from "../features/stacks/StackAccessScreen";
import CreateStackScreen from "../features/stacks/CreateStackScreen";
// The design system gallery is a development-only route, and deliberately a
// sibling of "/" rather than a child: everything under "/" renders inside
// SessionProvider, which cannot resolve without a signed-in session, so a
// nested styleguide would never mount locally.
//
// It is imported dynamically inside the DEV branch rather than statically at
// the top of this file. A static import would tree-shake its JavaScript out
// of production but NOT its stylesheet — CSS imports are side effects and
// survive tree-shaking, which shipped ~3KB of gallery layout to every user.
// With the import inside a statically-false branch, Rollup drops the chunk
// entirely and no CSS is emitted.
const devRoutes: RouteObject[] = import.meta.env.DEV
  ? [
      {
        path: "/styleguide",
        lazy: async () => ({ Component: (await import("../dev/StyleGuide")).default })
      }
    ]
  : [];

// "/" is not a screen — the design spec's route map has no row for it — but
// it is where a bare sign-in and a manually typed origin both land, so it has
// to send the visitor somewhere. This is a loader redirect rather than a
// <Navigate> element so react-router resolves it before rendering anything:
// the shell never flashes an empty index.
//
// Routes with a capability in the parent spec's route map are wrapped in a <RequireCapability mode="route"> layout route —
// see docs/superpowers/specs/2026-07-19-capability-gating-primitives-design.md.
// The pages redesigned on openplan UI, which the shell puts on the grey canvas.
const canvas: RouteHandle = { canvas: true };

export const routeConfig: RouteObject[] = [
  ...devRoutes,
  // A sibling of "/", not a child, for the same reason /styleguide is one:
  // everything under "/" renders inside SessionProvider, which resolves only
  // with a session — and this is the screen you are shown because you have
  // none. Nested, it could never mount.
  { path: "/signin", element: <SignInScreen /> },
  {
    path: "/",
    element: <SessionProvider />,
    children: [
      {
        element: <AppShell />,
        children: [
          { index: true, loader: () => redirect("/stacks") },
          { path: "stacks", element: <StacksListScreen />, handle: canvas },
          // A static segment, so it outranks stacks/:stackId below.
          { path: "stacks/attention", element: <StackAttentionScreen />, handle: canvas },
          {
            path: "stacks/new",
            element: <RequireCapability capability="canCreateStack" mode="route" />,
            children: [{ index: true, element: <CreateStackScreen /> }]
          },
          {
            path: "stacks/:stackId",
            element: <RequireCapability capability="canView" mode="route" />,
            children: [
              // The stack's one page: its templates, and the selected one's
              // panel, which every route below draws into.
              {
                element: <StackPage />,
                handle: canvas,
                children: [
                  { index: true, element: <StackIndexPanel /> },
                  // The old template list is the stack's page now.
                  { path: "templates", loader: ({ params }) => redirect(`/stacks/${params.stackId}`) },
                  {
                    path: "templates/new",
                    element: <RequireCapability capability="canOperate" mode="route" />,
                    children: [{ index: true, element: <AddStackTemplateScreen /> }]
                  },
                  // A run nests under runs/ so Runs stays lit while reading
                  // one, and Change revision under the template so Settings
                  // does; the index sends you to Runs.
                  {
                    path: "templates/:stackTemplateId",
                    element: <TemplatePanel />,
                    children: [
                      { index: true, element: <Navigate to="runs" replace /> },
                      { path: "runs", element: <TemplateRunsTab /> },
                      { path: "runs/:runNumber", element: <RunDetailScreen /> },
                      { path: "variables", element: <TemplateVariablesTab /> },
                      {
                        path: "credentials",
                        element: <RequireCapability capability="canManageAccess" mode="route" />,
                        children: [{ index: true, element: <TemplateCredentialsTab /> }]
                      },
                      { path: "settings", element: <TemplateSettingsTab /> },
                      {
                        path: "upgrade",
                        element: <RequireCapability capability="canOperate" mode="route" />,
                        children: [{ index: true, element: <UpgradeStackTemplateScreen /> }]
                      }
                    ]
                  }
                ]
              },
              // Environment and Access keep their pages, under a breadcrumb.
              {
                element: <StackSectionLayout />,
                children: [
                  {
                    path: "environment",
                    element: <RequireCapability capability="canManageAccess" mode="route" />,
                    children: [{ index: true, element: <EnvironmentScreen /> }]
                  },
                  {
                    path: "access",
                    element: <RequireCapability capability="canManageAccess" mode="route" />,
                    children: [{ index: true, element: <StackAccessScreen /> }]
                  }
                ]
              }
            ]
          },
          { path: "templates", element: <TemplateRegistryScreen /> },
          { path: "templates/new", element: <TemplateRegistrationScreen /> },
          // After "templates/new", so the static segment is matched first
          // rather than being read as a source template id.
          { path: "templates/:sourceTemplateId", element: <TemplateDetailScreen /> },
          { path: "*", element: <NotFound /> }
        ]
      }
    ]
  }
];

// createBrowserRouter touches `document` as soon as it's called, which blows
// up when this module is evaluated under Vitest's node environment (used by
// router.test.tsx to read `routeConfig`) or any other non-browser context.
// Fall back to a memory router there; both factories return the same Router
// type, so the browser (and Task 4's main.tsx) always gets a real
// createBrowserRouter(routeConfig) instance.
export const router =
  typeof document === "undefined" ? createMemoryRouter(routeConfig) : createBrowserRouter(routeConfig);
