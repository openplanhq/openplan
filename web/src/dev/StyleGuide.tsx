import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import HeroGraphic from "../shared/HeroGraphic";
import { LogStep, LogSteps } from "../shared/LogSteps";
import StatusRow from "../shared/StatusRow";
import { statusTone } from "../shared/statusTone";
import RoleBadge from "../shared/RoleBadge";
import StatusBadge from "../shared/StatusBadge";
import "./styleguide.css";
import { Plus } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
  { id: "tabs", title: "Tabs" },
  { id: "tables", title: "Tables" },
  { id: "log", title: "Log panel" },
  { id: "showpiece", title: "Showpieces" }
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
const ROLES = ["owner", "operator", "approver", "viewer"];
const titleCase = (word: string) => word[0].toUpperCase() + word.slice(1);

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

function LogStepsSpecimen() {
  const [open, setOpen] = useState<Record<string, boolean>>({ plan: true });
  const toggle = (name: string) => setOpen((current) => ({ ...current, [name]: !current[name] }));
  return (
    <LogSteps>
      <LogStep name="plan-init" open={open["plan-init"] ?? false} onToggle={() => toggle("plan-init")}>
        {`Initializing the backend...
Successfully configured the backend "s3"!`}
      </LogStep>
      <LogStep name="plan" open={open.plan ?? false} onToggle={() => toggle("plan")}>
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
              {ROLES.map((role) => (
                <RoleBadge key={role} role={role} />
              ))}
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
            <button type="button" className="icon-button" aria-label="Remove">
              ✕
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

        <Section
          id="tables"
          title="Tables"
          note="Fixed layout: each column takes its width from a <col>, one column takes the slack, and cell contents never resize a column. Long values end in an ellipsis. The frame scrolls sideways; the page never does."
        >
          <Specimen label="data-table" hint="xs · sm · lg · md · slack · actions" stack>
            <div className="data-table-frame">
              <table className="data-table" style={{ ["--legacy-data-table-min-width" as string]: "1040px" }}>
                <colgroup>
                  <col className="data-table__col--xs" />
                  <col className="data-table__col--sm" />
                  <col className="data-table__col--lg" />
                  <col className="data-table__col--md" />
                  <col />
                  <col className="data-table__col--actions" />
                </colgroup>
                <thead>
                  <tr>
                    <th scope="col">Run</th>
                    <th scope="col">Type</th>
                    <th scope="col">Status</th>
                    <th scope="col">Actor</th>
                    <th scope="col">Time</th>
                    <th scope="col">
                      <span className="visually-hidden">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {[
                    { number: 3, operation: "apply", status: "waiting_approval", actor: "a.really-long-username@example.com" },
                    { number: 2, operation: "plan", status: "completed", actor: "admin@openplan.local" },
                    { number: 1, operation: "plan", status: "failed", actor: "admin@openplan.local" }
                  ].map((row) => (
                    <tr key={row.number}>
                      <td>
                        <a className="data-table__link" href="#tables">
                          #{row.number}
                        </a>
                      </td>
                      <td>{row.operation}</td>
                      <td>
                        <StatusBadge tone={statusTone(row.status)}>{row.status}</StatusBadge>
                      </td>
                      <td className="data-table__mono" title={row.actor}>
                        {row.actor}
                      </td>
                      <td className="data-table__mono">21 Sept, 10:5{row.number}</td>
                      <td className="data-table__actions">
                        {row.number === 3 && (
                          <>
                            <button className="secondary-button" type="button">
                              Cancel
                            </button>
                            <button className="primary-button" type="button">
                              Approve
                            </button>
                          </>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Specimen>
        </Section>

        <Section
          id="log"
          title="Log panel"
          note="Phases stack in the order they ran, each a row that opens onto its log. The log deliberately carries no texture: patterning behind a monospace log stream measurably hurts scanning for errors."
        >
          <Specimen label="LogSteps" hint="real component" stack>
            <LogStepsSpecimen />
          </Specimen>
        </Section>

        <Section
          id="showpiece"
          title="Showpieces"
          note="The hero graphic is decorative and aria-hidden. Its ring rotates over 60 seconds and its cards drift on offset timings; all of it stops under prefers-reduced-motion."
        >
          <Specimen label="HeroGraphic" hint="real component">
            <HeroGraphic />
          </Specimen>

          <Specimen label="showcase" stack>
            <section className="showcase" style={{ minHeight: 0 }}>
              <div className="showcase__body">
                <h1 className="showcase__title gradient-text">Authorization service unavailable</h1>
                <p className="showcase__lede">
                  We could not reach the authorization service. Retry in a moment.
                </p>
                <a className="secondary-button" href="#showpiece">
                  Back to stacks
                </a>
              </div>
              <div className="showcase__visual">
                <HeroGraphic />
              </div>
            </section>
          </Specimen>
        </Section>
      </main>
    </div>
  );
}
