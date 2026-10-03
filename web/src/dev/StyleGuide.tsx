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
 * cannot drift from them, and it reads theme values out of the cascade at
 * runtime rather than restating them, so the swatches cannot drift from
 * theme.css either.
 *
 * It is mounted OUTSIDE SessionProvider (see app/router.tsx) so it renders
 * with no identity provider and no backend — the app itself cannot mount without
 * them, which makes this the only way to see the design system locally.
 */

const SECTIONS: { id: string; title: string }[] = [
  { id: "theme", title: "shadcn theme" },
  { id: "colour", title: "Colour" },
  { id: "type", title: "Typography" },
  { id: "radii", title: "Radii" }
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

const COLOUR_TOKENS = THEME_SWATCHES.map(({ name }) => `--${name}`);

const RADIUS_TOKENS = ["--radius", "--radius-sm", "--radius-md", "--radius-lg", "--radius-xl"];

const TYPE_STEPS = ["--text-xs", "--text-sm", "--text-base", "--text-lg", "--text-xl", "--text-2xl", "--text-3xl"];

const FONT_TOKENS = ["--font-sans", "--font-mono"];

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
    <section className="grid gap-6 border-t pt-6" id={id}>
      <h2 className="font-heading text-xl leading-snug font-medium tracking-normal">{title}</h2>
      {note && <p className="max-w-3xl text-sm text-muted-foreground">{note}</p>}
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
    <div className="grid gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span className="text-xs font-medium">{label}</span>
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      </div>
      <div className={cn("flex flex-wrap items-center gap-3", stack && "flex-col items-start")}>{children}</div>
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
  const type = useTokenValues(TYPE_STEPS);
  const fonts = useTokenValues(FONT_TOKENS);

  return (
    <div className="min-h-svh md:flex" data-testid="styleguide">
      <aside className="border-b bg-card p-6 md:sticky md:top-0 md:max-h-svh md:w-60 md:shrink-0 md:self-start md:overflow-y-auto md:border-r md:border-b-0">
        <div className="mb-6 font-heading text-xl leading-tight">
          openplan
          <span className="block font-mono text-xs tracking-wide text-muted-foreground uppercase">Design system</span>
        </div>
        <nav aria-label="Design system sections">
          <ol className="m-0 grid list-none gap-1 p-0">
            {SECTIONS.map((section) => (
              <li key={section.id}>
                <a
                  className="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  href={`#${section.id}`}
                >
                  {section.title}
                </a>
              </li>
            ))}
          </ol>
        </nav>
      </aside>

      <main className="grid min-w-0 flex-1 content-start gap-10 p-6 md:p-10">
        <div className="grid gap-3">
          <h1 className="font-heading text-2xl leading-tight font-medium tracking-tight sm:text-4xl">Design system</h1>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Every example below renders the real component and reads its theme values out of the cascade, so this page
            cannot drift from the implementation. It is registered only in development builds, and it mounts outside the
            auth provider so it works with no backend.
          </p>
        </div>

        <div className="grid gap-10" data-testid="sg-theme">
          <Section
            id="theme"
            title="shadcn theme"
            note="Components from src/components/ui on theme.css, the only file that carries raw colour values."
          >
            <Specimen label="Colours" hint="theme.css">
              <div className="grid w-full grid-cols-2 gap-3 sm:grid-cols-5">
                {THEME_SWATCHES.map(({ name, className }) => (
                  <div key={name} data-swatch={name} className="flex flex-col gap-1.5">
                    <div className={cn("h-10 rounded-md border", className)} />
                    <code className="font-mono text-xs text-muted-foreground">--{name}</code>
                    <code className="font-mono text-xs text-muted-foreground">{colours[`--${name}`] || "—"}</code>
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
            <Specimen label="Tabs" hint="line variant" stack>
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

          <Section
            id="colour"
            title="Colour"
            note="The primary is the only saturated colour in ordinary use. The three status colours are reserved for state, and each clears AA against its own tint, which is how StatusBadge and RoleBadge paint them."
          >
            <Specimen label="Palette" hint="values read from theme.css at runtime">
              <div className="grid w-full grid-cols-2 gap-4 sm:grid-cols-4">
                {COLOUR_TOKENS.map((token) => (
                  <dl className="m-0 grid gap-1" key={token}>
                    <div className="h-12 rounded-md border" style={{ background: `var(${token})` }} aria-hidden="true" />
                    <dt className="font-mono text-xs">{token}</dt>
                    <dd className="m-0 font-mono text-xs text-muted-foreground">{colours[token] || "—"}</dd>
                  </dl>
                ))}
              </div>
            </Specimen>
          </Section>

          <Section
            id="type"
            title="Typography"
            note="Geist carries headings, body and UI; Geist Mono carries every technical signal — labels, IDs, timestamps, status and logs."
          >
            <Specimen label="Families" stack>
              <p className="m-0 font-heading text-2xl">Geist display</p>
              <p className="m-0 text-lg">Geist body — the quick brown fox jumps over the lazy dog, 0123456789</p>
              <p className="m-0 font-mono text-sm">Geist Mono — stack_1a2b3c · 2026-08-11T09:14:22Z</p>
              <dl className="m-0 grid gap-1">
                {FONT_TOKENS.map((token) => (
                  <div className="flex flex-wrap gap-2" key={token}>
                    <dt className="font-mono text-xs">{token}</dt>
                    <dd className="m-0 font-mono text-xs text-muted-foreground">{fonts[token] || "—"}</dd>
                  </div>
                ))}
              </dl>
            </Specimen>

            <Specimen label="Scale" stack>
              <dl className="m-0 grid w-full gap-2">
                {TYPE_STEPS.map((step) => (
                  <div className="flex flex-wrap items-baseline gap-4 border-b pb-2" key={step}>
                    <dt className="w-56 shrink-0 font-mono text-xs text-muted-foreground">
                      {step} · {type[step] || "—"}
                    </dt>
                    <dd className="m-0" style={{ fontSize: `var(${step})` }}>
                      Terraform
                    </dd>
                  </div>
                ))}
              </dl>
            </Specimen>
          </Section>

          <Section
            id="radii"
            title="Radii"
            note="Every radius derives from --radius, which components.json records as shadcn's stock 0.625rem."
          >
            <Specimen label="Scale">
              {RADIUS_TOKENS.map((token) => (
                <div key={token} className="grid justify-items-center gap-2">
                  <div className="size-16 border bg-muted" style={{ borderRadius: `var(${token})` }} />
                  <span className="font-mono text-xs">{token}</span>
                  <span className="font-mono text-xs text-muted-foreground">{radii[token] || "—"}</span>
                </div>
              ))}
            </Specimen>
          </Section>
        </div>
      </main>
    </div>
  );
}