import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { MemoryRouter, useInRouterContext } from "react-router-dom";
import RouteMessage from "../app/RouteMessage";
import Breadcrumb from "../shared/Breadcrumb";
import { LogStep, LogSteps } from "../shared/LogSteps";
import StatusRow from "../shared/StatusRow";
import { statusTone } from "../shared/statusTone";
import RoleBadge from "../shared/RoleBadge";
import { STACK_ROLES } from "../shared/roles";
import StatusBadge from "../shared/StatusBadge";
import "./styleguide.css";
import { CircleAlert, Info, Plus, Search, SearchX } from "lucide-react";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from "@/components/ui/combobox";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ScrollArea, ScrollAreaContent, ScrollAreaScrollbar, ScrollAreaThumb, ScrollAreaViewport } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

/**
 * Development-only gallery of the design system.
 *
 * It renders the real components rather than copies of their markup, so it
 * cannot drift from them, and it reads token values out of the cascade at
 * runtime rather than restating them, so the swatches cannot drift from
 * tokens.css either.
 *
 * It is mounted OUTSIDE SessionProvider (see app/router.tsx) so it renders
 * with no identity provider and no backend — the app itself cannot mount without
 * them, which makes this the only way to see the design system locally.
 */

const SECTIONS: { id: string; title: string }[] = [
  { id: "theme", title: "shadcn theme" },
  { id: "colour", title: "Colour" },
  { id: "type", title: "Typography" },
  { id: "radii", title: "Radii" },
  { id: "shadows", title: "Shadows" },
  { id: "buttons", title: "Buttons" },
  { id: "inputs", title: "Inputs" },
  { id: "panels", title: "Panels" },
  { id: "messaging", title: "Messaging" },
  { id: "tabs", title: "Tabs" }
];

// Literal class names: Tailwind only generates classes it can find in source.
const THEME_SWATCHES = [
  { name: "background", className: "bg-background" },
  { name: "foreground", className: "bg-foreground" },
  { name: "primary", className: "bg-primary" },
  { name: "secondary", className: "bg-secondary" },
  { name: "muted", className: "bg-muted" },
  { name: "accent", className: "bg-accent" },
  { name: "destructive", className: "bg-destructive" },
  { name: "success", className: "bg-success" },
  { name: "warning", className: "bg-warning" },
  { name: "border", className: "bg-border" }
];

const BUTTON_VARIANTS = ["default", "outline", "secondary", "ghost", "destructive", "link"] as const;
const BUTTON_SIZES = ["xs", "sm", "default", "lg"] as const;
const BADGE_VARIANTS = ["default", "secondary", "outline", "destructive", "success", "progress", "warning", "muted"] as const;
// One status per tone: settled, progress, waiting, failed, canceled.
const STATUS_SAMPLES = ["completed", "running", "waiting_approval", "failed", "canceled"];
const titleCase = (word: string) => word[0].toUpperCase() + word.slice(1);
const STACK_NAMES = ["payments-core", "payments-edge", "billing", "identity", "search"];

const COLOUR_TOKENS = [
  "--legacy-color-bg",
  "--legacy-color-fg",
  "--legacy-color-card",
  "--legacy-color-muted",
  "--legacy-color-muted-fg",
  "--legacy-color-border",
  "--legacy-color-accent",
  "--legacy-color-accent-2",
  "--legacy-color-success",
  "--legacy-color-warning",
  "--legacy-color-danger",
  "--legacy-color-success-dot",
  "--legacy-color-warning-dot"
];

const RADIUS_TOKENS = ["--legacy-radius-sm", "--legacy-radius-md", "--legacy-radius-lg", "--legacy-radius-xl", "--legacy-radius-full"];

const SHADOW_TOKENS = [
  "--legacy-shadow-sm",
  "--legacy-shadow-md",
  "--legacy-shadow-lg",
  "--legacy-shadow-xl",
  "--legacy-shadow-accent",
  "--legacy-shadow-accent-lg"
];

const TYPE_STEPS = [
  "--legacy-text-xs",
  "--legacy-text-sm",
  "--legacy-text-base",
  "--legacy-text-lg",
  "--legacy-text-xl",
  "--legacy-text-2xl",
  "--legacy-text-3xl",
  "--legacy-text-4xl",
  "--legacy-text-5xl"
];

/** Reads custom properties off the document root, so nothing is restated here. */
function useTokenValues(names: string[]): Record<string, string> {
  const [values, setValues] = useState<Record<string, string>>({});

  useEffect(() => {
    const computed = getComputedStyle(document.documentElement);
    const next: Record<string, string> = {};
    for (const name of names) {
      next[name] = computed.getPropertyValue(name).trim();
    }
    setValues(next);
    // names is a module-level constant array per call site; re-reading on every
    // render would be wasteful and the tokens cannot change at runtime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return values;
}

function Section({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section className="sg__section" id={id}>
      <h2>{title}</h2>
      {note && <p className="sg__note">{note}</p>}
      {children}
    </section>
  );
}

function Specimen({
  label,
  hint,
  stack = false,
  children
}: {
  label: string;
  hint?: string;
  stack?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="sg__specimen">
      <div className="sg__specimen-label">
        <span>{label}</span>
        {hint && <span>{hint}</span>}
      </div>
      <div className={`sg__specimen-body${stack ? " sg__specimen-body--stack" : ""}`}>{children}</div>
    </div>
  );
}

// Breadcrumb links need a router. The app mounts this page inside its own
// router, and a second one there would throw; the standalone test has none.
function WithRouter({ children }: { children: ReactNode }) {
  return useInRouterContext() ? <>{children}</> : <MemoryRouter>{children}</MemoryRouter>;
}

function LogStepsSpecimen() {
  const [open, setOpen] = useState<Record<string, boolean>>({ plan: true });
  const setStep = (name: string) => (isOpen: boolean) => setOpen((current) => ({ ...current, [name]: isOpen }));
  return (
    <LogSteps>
      <LogStep name="plan-init" open={open["plan-init"] ?? false} onOpenChange={setStep("plan-init")}>
        {`Initializing the backend...
Successfully configured the backend "s3"!`}
      </LogStep>
      <LogStep name="plan" open={open.plan ?? false} onOpenChange={setStep("plan")}>
        {`Initializing the backend...
Terraform v1.9.5 on darwin_arm64
Plan: 3 to add, 1 to change, 0 to destroy.
Error: creating S3 Bucket: BucketAlreadyExists`}
      </LogStep>
    </LogSteps>
  );
}

export default function StyleGuide() {
  const colours = useTokenValues(COLOUR_TOKENS);
  const radii = useTokenValues(RADIUS_TOKENS);
  const shadows = useTokenValues(SHADOW_TOKENS);
  const type = useTokenValues(TYPE_STEPS);

  return (
    <div className="sg" data-testid="styleguide">
      <aside className="sg__nav">
        <div className="sg__brand">
          openplan
          <span>Design system</span>
        </div>
        <nav aria-label="Design system sections">
          <ol>
            {SECTIONS.map((section) => (
              <li key={section.id}>
                <a href={`#${section.id}`}>{section.title}</a>
              </li>
            ))}
          </ol>
        </nav>
      </aside>

      <main className="sg__main">
        <div className="sg__intro">
          <h1>Design system</h1>
          <p>
            Every example below renders the real component and reads its token values out of the
            cascade, so this page cannot drift from the implementation. It is registered only in
            development builds, and it mounts outside the auth provider so it works with no backend.
          </p>
        </div>

        <div data-testid="sg-theme">
          <Section
            id="theme"
            title="shadcn theme"
            note="Components from src/components/ui on theme.css. Everything below this section is the legacy system, retired screen by screen."
          >
            <Specimen label="Colours" hint="theme.css">
              <div className="grid w-full grid-cols-2 gap-3 sm:grid-cols-5">
                {THEME_SWATCHES.map(({ name, className }) => (
                  <div key={name} data-swatch={name} className="flex flex-col gap-1.5">
                    <div className={cn("h-10 rounded-md border", className)} />
                    <code className="font-mono text-xs text-muted-foreground">--{name}</code>
                  </div>
                ))}
              </div>
            </Specimen>
            <Specimen label="Button variants">
              {BUTTON_VARIANTS.map((variant) => (
                <Button key={variant} variant={variant}>
                  {titleCase(variant)}
                </Button>
              ))}
            </Specimen>
            <Specimen label="Button sizes">
              {BUTTON_SIZES.map((size) => (
                <Button key={size} size={size} variant="outline">
                  {size}
                </Button>
              ))}
              <Button size="icon" aria-label="Add">
                <Plus />
              </Button>
            </Specimen>
            <Specimen label="Badge variants">
              {BADGE_VARIANTS.map((variant) => (
                <Badge key={variant} variant={variant}>
                  {variant}
                </Badge>
              ))}
            </Specimen>
            <Specimen label="StatusBadge" hint="real component, tone from statusTone()">
              {STATUS_SAMPLES.map((status) => (
                <StatusBadge key={status} tone={statusTone(status)}>
                  {status}
                </StatusBadge>
              ))}
            </Specimen>
            <Specimen label="StatusRow" hint="real component" stack>
              <div className="w-full max-w-md">
                <StatusRow label="Template" value="completed" />
                <StatusRow label="Plan" value="running" />
                <StatusRow label="Approval" value="waiting_approval" />
                <StatusRow label="Apply" value="failed" />
                <StatusRow label="Cancelled" value="canceled" />
                <StatusRow label="Approved" value="approved" />
              </div>
            </Specimen>
            <Specimen label="RoleBadge" hint="real component">
              {STACK_ROLES.map(({ value }) => (
                <RoleBadge key={value} stackRole={value} />
              ))}
            </Specimen>
            <Specimen label="Breadcrumb" hint="real component" stack>
              <WithRouter>
                <Breadcrumb
                  className="mb-0"
                  items={[{ label: "Stacks", to: "#theme" }, { label: "payments", to: "#theme" }, { label: "Run #4" }]}
                  detail="acme/vpc · main"
                />
              </WithRouter>
            </Specimen>
            <Specimen label="LogSteps" hint="real component, on Collapsible" stack>
              <LogStepsSpecimen />
            </Specimen>
            <Specimen label="Card" stack>
              <Card className="w-full max-w-sm">
                <CardHeader>
                  <CardTitle>payments-core</CardTitle>
                  <CardDescription>3 templates · last run 21 Sept</CardDescription>
                </CardHeader>
                <CardContent>
                  <p className="text-muted-foreground">One subject per card: a sign-in, a summary, a form.</p>
                </CardContent>
              </Card>
            </Specimen>
            <Specimen label="Alert" stack>
              <div className="grid w-full max-w-md gap-3">
                <Alert>
                  <Info />
                  <AlertTitle>Plan queued</AlertTitle>
                  <AlertDescription>It starts when a worker is free.</AlertDescription>
                </Alert>
                <Alert variant="destructive">
                  <CircleAlert />
                  <AlertTitle>Incorrect username or password.</AlertTitle>
                </Alert>
              </div>
            </Specimen>
            <Specimen label="Input and Label" stack>
              <div className="grid w-full max-w-sm gap-4">
                <div className="grid gap-2">
                  <Label htmlFor="sg-input">Stack name</Label>
                  <Input id="sg-input" placeholder="payments-core" />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="sg-input-invalid">Invalid</Label>
                  <Input id="sg-input-invalid" defaultValue="Payments Core" aria-invalid />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="sg-input-disabled">Disabled</Label>
                  <Input id="sg-input-disabled" defaultValue="payments-core" disabled />
                </div>
              </div>
            </Specimen>
            <Specimen label="Empty" stack>
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <SearchX />
                  </EmptyMedia>
                  <EmptyTitle>No stacks yet</EmptyTitle>
                  <EmptyDescription>Stacks you can see appear here.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            </Specimen>
            <Specimen label="RouteMessage" hint="real component, on Empty" stack>
              <RouteMessage
                icon={SearchX}
                title="Page not found"
                description="The page you were looking for doesn't exist."
                testId="sg-route-message"
              />
            </Specimen>
            <Specimen label="Table" hint="fixed layout: widths in a <colgroup>, one column takes the slack" stack>
              <div className="w-full overflow-hidden rounded-lg border">
                <Table>
                  <colgroup>
                    <col />
                    <col className="w-48" />
                  </colgroup>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Slug</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {[
                      ["Payments", "payments"],
                      ["A stack whose name is too long for its column", "a-stack-whose-name-is-too-long-for-its-column"]
                    ].map(([name, slug]) => (
                      <TableRow key={slug}>
                        <TableCell className="truncate font-medium" title={name}>
                          {name}
                        </TableCell>
                        <TableCell className="truncate font-mono text-xs text-muted-foreground" title={slug}>
                          {slug}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </Specimen>
            <Specimen label="AlertDialog" hint="modal confirmation with focus containment" stack>
              <AlertDialog>
                <AlertDialogTrigger
                  data-testid="styleguide-alert-dialog-trigger"
                  render={<Button variant="destructive" className="pointer-coarse:h-11" />}
                >
                  Destroy 4
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <div className="grid gap-2">
                    <AlertDialogTitle>Destroy 4 resources?</AlertDialogTitle>
                    <AlertDialogDescription>This action cannot be undone.</AlertDialogDescription>
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <AlertDialogClose render={<Button variant="outline" className="pointer-coarse:h-11" />}>
                      Cancel
                    </AlertDialogClose>
                    <AlertDialogClose render={<Button variant="destructive" className="pointer-coarse:h-11" />}>
                      Confirm destroy
                    </AlertDialogClose>
                  </div>
                </AlertDialogContent>
              </AlertDialog>
            </Specimen>
            <Specimen label="ScrollArea" hint="scrollable run log" stack>
              <ScrollArea className="max-h-40 w-full max-w-md rounded-lg border bg-foreground" data-testid="styleguide-scroll-area">
                <ScrollAreaViewport className="max-h-40 overflow-auto">
                  <ScrollAreaContent className="p-4">
                    <pre className="m-0 font-mono text-xs whitespace-pre-wrap text-background">
                      {Array.from({ length: 16 }, (_, index) => `plan-${index + 1}: Terraform resource details`).join("\n")}
                    </pre>
                  </ScrollAreaContent>
                </ScrollAreaViewport>
                <ScrollAreaScrollbar>
                  <ScrollAreaThumb />
                </ScrollAreaScrollbar>
              </ScrollArea>
            </Specimen>
            <Specimen label="Tabs" hint="line variant; RouteTabs renders each tab as a link" stack>
              <Tabs defaultValue="templates">
                <TabsList variant="line" aria-label="Tabs specimen">
                  <TabsTrigger value="overview">Overview</TabsTrigger>
                  <TabsTrigger value="templates">Templates</TabsTrigger>
                  <TabsTrigger value="environment">Environment</TabsTrigger>
                </TabsList>
                <TabsContent value="overview">The stack at a glance.</TabsContent>
                <TabsContent value="templates">The templates installed on this stack.</TabsContent>
                <TabsContent value="environment">Credentials every template on this stack receives.</TabsContent>
              </Tabs>
            </Specimen>
            <Specimen label="Combobox" hint="filters these five as you type; the stack-access search asks the server instead" stack>
              <Combobox items={STACK_NAMES}>
                <ComboboxInput aria-label="Combobox specimen" placeholder="Find a stack" showTrigger={false} className="w-full max-w-sm" />
                <ComboboxContent>
                  <ComboboxEmpty>No stacks found</ComboboxEmpty>
                  <ComboboxList>
                    {(name: string) => (
                      <ComboboxItem key={name} value={name}>
                        {name}
                      </ComboboxItem>
                    )}
                  </ComboboxList>
                </ComboboxContent>
              </Combobox>
            </Specimen>
            <Specimen label="Select" stack>
              <div className="grid w-full max-w-sm gap-2">
                <Label htmlFor="sg-select">Default role</Label>
                <Select items={STACK_ROLES} defaultValue="viewer">
                  <SelectTrigger id="sg-select" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {STACK_ROLES.map((role) => (
                      <SelectItem key={role.value} value={role.value}>
                        {role.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Specimen>
            <Specimen label="Input group" hint="Combobox's input is one" stack>
              <InputGroup className="max-w-sm">
                <InputGroupAddon>
                  <Search />
                </InputGroupAddon>
                <InputGroupInput aria-label="Input group specimen" placeholder="Search stacks" />
              </InputGroup>
            </Specimen>
            <Specimen label="Textarea" stack>
              <div className="grid w-full max-w-sm gap-2">
                <Label htmlFor="sg-textarea">Description</Label>
                <Textarea id="sg-textarea" placeholder="What this stack is for" />
              </div>
            </Specimen>
          </Section>
        </div>

        <Section
          id="colour"
          title="Colour"
          note="The accent is the only saturated colour in ordinary use. The three status colours are reserved for state. Note that --legacy-color-success-dot and --legacy-color-warning-dot are brighter variants restricted to dots and fills: they measure roughly 3.2:1 on white and fail AA for text, which is why the text-bearing tokens are darker."
        >
          <Specimen label="Palette" hint="values read from tokens.css at runtime">
            <div className="sg__swatches">
              {COLOUR_TOKENS.map((token) => (
                <dl className="sg__swatch" key={token}>
                  <div
                    className="sg__swatch-chip"
                    style={{ background: `var(${token})` }}
                    aria-hidden="true"
                  />
                  <dt>{token}</dt>
                  <dd>{colours[token] || "—"}</dd>
                </dl>
              ))}
            </div>
          </Specimen>

          <Specimen label="Gradient" hint="--legacy-gradient-accent">
            <div
              style={{
                width: "100%",
                height: "72px",
                borderRadius: "var(--legacy-radius-lg)",
                background: "var(--legacy-gradient-accent)"
              }}
            />
          </Specimen>
        </Section>

        <Section
          id="type"
          title="Typography"
          note="Geist carries headings, body and UI; Geist Mono carries every technical signal — labels, IDs, timestamps, status and logs."
        >
          <Specimen label="Families" stack>
            <p style={{ fontFamily: "var(--legacy-font-display)", fontWeight: 400, fontSize: "var(--legacy-text-4xl)", margin: 0 }}>
              Geist display
            </p>
            <p style={{ fontFamily: "var(--legacy-font-body)", fontSize: "var(--legacy-text-lg)", margin: "var(--legacy-space-4) 0 0" }}>
              Geist body — the quick brown fox jumps over the lazy dog, 0123456789
            </p>
            <p style={{ fontFamily: "var(--legacy-font-mono)", fontSize: "var(--legacy-text-sm)", margin: "var(--legacy-space-4) 0 0" }}>
              Geist Mono — stack_1a2b3c · 2026-08-11T09:14:22Z
            </p>
          </Specimen>

          <Specimen label="Scale" stack>
            <dl className="sg__rows">
              {TYPE_STEPS.map((step) => (
                <div className="sg__row" key={step}>
                  <dt className="sg__row-name">
                    {step} · {type[step] || "—"}
                  </dt>
                  <dd style={{ margin: 0, fontSize: `var(${step})`, lineHeight: "var(--legacy-leading-tight)" }}>
                    Terraform
                  </dd>
                </div>
              ))}
            </dl>
          </Specimen>
        </Section>

        <Section id="radii" title="Radii">
          <Specimen label="Scale">
            {RADIUS_TOKENS.map((token) => (
              <div key={token} style={{ display: "grid", gap: "var(--legacy-space-2)", justifyItems: "center" }}>
                <div className="sg__radius-tile" style={{ borderRadius: `var(${token})` }} />
                <span className="sg__row-name">{token}</span>
                <span className="sg__row-name">{radii[token] || "—"}</span>
              </div>
            ))}
          </Specimen>
        </Section>

        <Section
          id="shadows"
          title="Shadows"
          note="Every box-shadow in the codebase must reference one of these tokens; a guard test fails the build otherwise."
        >
          <Specimen label="Scale" stack>
            <dl className="sg__rows">
              {SHADOW_TOKENS.map((token) => (
                <div className="sg__row" key={token}>
                  <dt className="sg__row-name">{token}</dt>
                  <dd style={{ margin: 0 }}>
                    <div className="sg__shadow-tile" style={{ boxShadow: `var(${token})` }} />
                  </dd>
                </div>
              ))}
            </dl>
          </Specimen>
        </Section>

        <Section
          id="buttons"
          title="Buttons"
          note="Primary carries the gradient and lifts on hover. Destructive stays neutral-weight until a confirm step sets data-confirming, so the red never becomes ambient."
        >
          <Specimen label="Variants" hint="hover and focus them">
            <button type="button" className="primary-button">
              Run plan <span className="btn-arrow">→</span>
            </button>
            <button type="button" className="secondary-button">
              Add grant
            </button>
            <button type="button" className="destructive-button">
              Destroy <span className="btn-arrow">→</span>
            </button>
          </Specimen>

          <Specimen label="States">
            <button type="button" className="primary-button" disabled>
              Disabled
            </button>
            <button type="button" className="destructive-button" data-confirming="true">
              Confirm destroy
            </button>
          </Specimen>
        </Section>

        <Section id="inputs" title="Inputs" note="Focus draws a two-step accent ring rather than recolouring the field.">
          <Specimen label="Fields" stack>
            <label style={{ maxWidth: "360px" }}>
              Stack name
              <input placeholder="payments-core" />
            </label>
            <label style={{ maxWidth: "360px", marginTop: "var(--legacy-space-5)" }}>
              Description
              <textarea placeholder="What does this stack manage?" />
            </label>
            <label style={{ maxWidth: "360px", marginTop: "var(--legacy-space-5)" }}>
              Role
              <select defaultValue="operator">
                <option value="owner">owner</option>
                <option value="operator">operator</option>
                <option value="approver">approver</option>
                <option value="viewer">viewer</option>
              </select>
            </label>
          </Specimen>
        </Section>

        <Section
          id="panels"
          title="Panels"
          note="The featured variant paints its gradient border from a background layer, so it carries a forced-colors fallback; without one it would lose all emphasis in Windows High Contrast."
        >
          <Specimen label="Variants" stack>
            <div style={{ display: "grid", gap: "var(--legacy-space-5)" }}>
              <section className="panel">
                <h2>Standard</h2>
                <p className="muted">One border, one shadow, rounded corners.</p>
              </section>
              <section className="panel panel--featured">
                <h2>Featured</h2>
                <p className="muted">Gradient border, drawn with no wrapper element.</p>
              </section>
            </div>
          </Specimen>
        </Section>

        <Section id="messaging" title="Messaging">
          <Specimen label="Variants" stack>
            <div className="alert">Could not reach the authorization service.</div>
            <p className="error-text">Credential TF_VAR_token is not configured</p>
            <p className="muted">Secondary text uses the muted foreground.</p>
          </Specimen>
        </Section>

        <Section id="tabs" title="Tabs">
          <Specimen label="Active state" hint="gradient underline">
            <div className="stack-detail-tabs" style={{ marginBottom: 0 }}>
              <a href="#tabs" className="active">
                Overview
              </a>
              <a href="#tabs">Template</a>
              <a href="#tabs">Runs</a>
              <a href="#tabs">Access</a>
            </div>
          </Specimen>
        </Section>


      </main>
    </div>
  );
}
