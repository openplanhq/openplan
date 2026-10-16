import { RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { Outlet, useOutletContext, useParams } from "react-router-dom";
import { useStackQuery } from "../api/queries";
import { tenantID } from "../config";
import { buttonClass } from "../shared/buttonClass";
import ErrorLine from "../shared/ErrorLine";
import { classifyQueryError, useQueryErrorBoundary } from "../shared/queryErrorBoundary";
import { useAuth } from "./AuthContext";
import { useStackCapabilities } from "./useStackCapabilities";
import type { Me } from "./types";
import type { StackCapabilities } from "./types";
import NotFound from "../app/NotFound";
import AccessDenied from "../app/AccessDenied";
import { cn } from "@/lib/utils";

type GlobalCapabilityKey = keyof Me["globalCapabilities"];
type StackCapabilityKey = keyof StackCapabilities;
export type CapabilityKey = GlobalCapabilityKey | StackCapabilityKey;

const GLOBAL_CAPABILITY_KEYS: readonly GlobalCapabilityKey[] = [
  "isPlatformAdmin",
  "canCreateStack",
  "canPublishTemplate"
];

function isGlobalCapability(capability: CapabilityKey): capability is GlobalCapabilityKey {
  return (GLOBAL_CAPABILITY_KEYS as readonly string[]).includes(capability);
}

export interface RequireCapabilityProps {
  capability: CapabilityKey;
  stackId?: string;
  mode?: "gate" | "route";
  children?: ReactNode;
  fallback?: ReactNode;
}

type CapabilityState = "loading" | "allowed" | "denied";

function useCapabilityState(capability: CapabilityKey, stackId: string | undefined): CapabilityState {
  const { me, status } = useAuth();
  const params = useParams<{ stackId?: string }>();
  const resolvedStackId = stackId ?? params.stackId ?? "";
  const stackCapabilities = useStackCapabilities(resolvedStackId);

  if (isGlobalCapability(capability)) {
    if (status === "loading") {
      return "loading";
    }
    return me?.globalCapabilities[capability] ? "allowed" : "denied";
  }

  if (resolvedStackId === "" || stackCapabilities === undefined) {
    return "loading";
  }
  return stackCapabilities[capability] ? "allowed" : "denied";
}

export default function RequireCapability({ capability, stackId, mode = "gate", children, fallback }: RequireCapabilityProps) {
  const state = useCapabilityState(capability, stackId);
  const params = useParams<{ stackId?: string }>();
  // A route guard sits between a layout and the page it guards, so it hands
  // the layout's outlet context through; otherwise the guarded page would
  // lose it just for being guarded.
  const outletContext = useOutletContext();

  // Loading is a distinct outcome from "denied" in both modes — never flash
  // denied/fallback content before the underlying auth/stack query resolves.
  // A route guarded on a stack is the whole page, though, so if the stack
  // never arrives it says why rather than staying blank (UI-018).
  if (state === "loading") {
    const resolvedStackId = stackId ?? params.stackId ?? "";
    if (mode === "route" && !isGlobalCapability(capability) && resolvedStackId !== "") {
      return <StackRoutePending stackId={resolvedStackId} />;
    }
    return null;
  }

  if (mode === "route") {
    if (state === "denied") {
      return capability === "canView" ? <NotFound /> : <AccessDenied />;
    }
    return <Outlet context={outletContext} />;
  }

  if (state === "denied") {
    return <>{fallback ?? null}</>;
  }
  return <>{children}</>;
}

// A stack's page until its stack has loaded: nothing while it loads, and
// once it fails, what went wrong. Left blank, a stack that does not exist,
// that the person may not see, or that failed to load would be an empty page
// for good, with nothing to say why or to try again.
function StackRoutePending({ stackId }: { stackId: string }) {
  const stackQuery = useStackQuery(tenantID, stackId);
  const boundary = useQueryErrorBoundary(stackQuery.error);
  // A stack that is not there, or that the person may not see, answers the
  // same on every try, so the first answer is enough to say so; waiting out
  // the query's retries would leave the page blank for seconds first. It
  // reads as not found either way, as a canView denial does, so the page
  // never says that a stack it hides exists.
  const answer = classifyQueryError(stackQuery.error ?? stackQuery.failureReason);
  if (answer === 403 || answer === 404) {
    return <NotFound />;
  }
  // A 401 is the session ending: the boundary signs the person out, and the
  // sign-in page replaces this one.
  if (stackQuery.status !== "error" || answer === 401) {
    return null;
  }
  if (boundary !== null) {
    return <>{boundary}</>;
  }
  return (
    <section className="flex flex-col items-start gap-3" data-testid="stack-route-error">
      <ErrorLine>Something went wrong while loading this stack.</ErrorLine>
      <button
        type="button"
        className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
        data-testid="stack-route-retry"
        onClick={() => void stackQuery.refetch()}
      >
        <RefreshCw data-icon="inline-start" aria-hidden="true" />
        Retry
      </button>
    </section>
  );
}
