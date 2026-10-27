import type { PlanSummary, Stack, StackTemplate, TemplateRevision, TemplateVariable } from "../../api/types";
import { findSelectedID } from "../../shared/listSelection";
import type { StatusTone } from "../../shared/statusTone";

export function findSelectedStackTemplate(stackTemplates: StackTemplate[], selectedStackTemplateID: string): StackTemplate | null {
  return findSelectedID(stackTemplates, selectedStackTemplateID);
}

// No ref suffix: source_ref is part of a template's identity, so it read
// the same on every install of that template and separated nothing. The ref
// still shows in the revision panel, where it is about a specific commit.
export function stackTemplateLabel(stackTemplate: Pick<StackTemplate, "display_name" | "workspace_name">): string {
  return stackTemplate.display_name || stackTemplate.workspace_name;
}

export function canChangeStackTemplateRevision(stackTemplate: StackTemplate | null, templateRevision: TemplateRevision | null): boolean {
  if (!stackTemplate || !templateRevision || templateRevision.status !== "active") {
    return false;
  }
  if (stackTemplate.desired_template_revision_id === templateRevision.id) {
    return false;
  }
  return stackTemplate.template_id === templateRevision.template_id;
}

export interface RevisionChangeVariablePartition {
  added: TemplateVariable[];
  carried: TemplateVariable[];
  removed: TemplateVariable[];
}

/**
 * Revisions this stack template can change to: active revisions of the same
 * template, minus the one already desired. The API's order is
 * preserved, but it is not treated as commit ancestry or recency.
 */
export function revisionChangeCandidates(
  revisions: TemplateRevision[],
  stackTemplate: StackTemplate | null
): TemplateRevision[] {
  if (!stackTemplate) {
    return [];
  }
  return revisions.filter((revision) => canChangeStackTemplateRevision(stackTemplate, revision));
}

/**
 * Compares the installed revision's variables against a target revision's, by
 * name. Carried variables carry the *target* revision's definition, because
 * that is what the revision change will be validated against — a variable can stay
 * named the same while becoming required or changing type.
 */
export function partitionRevisionChangeVariables(
  currentVariables: TemplateVariable[],
  targetVariables: TemplateVariable[]
): RevisionChangeVariablePartition {
  const currentNames = new Set(currentVariables.map((variable) => variable.name));
  const targetNames = new Set(targetVariables.map((variable) => variable.name));
  return {
    added: targetVariables.filter((variable) => !currentNames.has(variable.name)),
    carried: targetVariables.filter((variable) => currentNames.has(variable.name)),
    removed: currentVariables.filter((variable) => !targetNames.has(variable.name))
  };
}

/**
 * Save is available when the edited values differ from what is stored on the
 * stack template. It takes no revision: the template screen always renders the
 * installed template's desired revision, so there is no selection that could
 * disagree with it.
 */
export function canSaveInstalledTemplateConfig(
  stackTemplate: StackTemplate | null,
  variables: TemplateVariable[],
  variableValues: Record<string, string>
): boolean {
  if (!stackTemplate) {
    return false;
  }
  return hasConfigChanged(stackTemplate, variables, variableValues);
}

// Terraform's simple types keep a plain input and are sent as typed: the
// server converts "3" to a number and "true" to a bool the way Terraform does.
const simpleVariableTypes = new Set(["string", "number", "bool"]);

// isStructuredVariable says whether a variable's value is edited as JSON.
// An untyped variable counts: Terraform reads a missing type as `any`, so its
// value can be a list or an object, and a plain string is typed in quotes.
export function isStructuredVariable(variable: Pick<TemplateVariable, "type_expression">): boolean {
  return !simpleVariableTypes.has(variable.type_expression.trim());
}

export function variableValuesFromConfig(config: Record<string, unknown>, variables: TemplateVariable[]): Record<string, string> {
  const values: Record<string, string> = {};
  for (const variable of variables) {
    const value = config[variable.name];
    if (value == null) {
      values[variable.name] = "";
    } else {
      values[variable.name] = isStructuredVariable(variable) ? JSON.stringify(value, null, 2) : String(value);
    }
  }
  return values;
}

export interface EncodedConfig {
  config: Record<string, unknown>;
  /** Field text that isn't valid JSON, by variable name. Such a variable is
      left out of config, so a caller must not send config while this has any. */
  errors: Record<string, string>;
}

// configFromVariableValues turns the field text into the request's config.
// Empty text means "not set", so the variable is left out and its default
// applies. The shape against the type is the server's check, not this one's.
export function configFromVariableValues(variables: TemplateVariable[], values: Record<string, string>): EncodedConfig {
  const config: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const variable of variables) {
    const text = values[variable.name]?.trim() ?? "";
    if (text === "") {
      continue;
    }
    if (!isStructuredVariable(variable)) {
      config[variable.name] = text;
      continue;
    }
    try {
      config[variable.name] = JSON.parse(text);
    } catch {
      errors[variable.name] = `${variable.name} is not valid JSON`;
    }
  }
  return { config, errors };
}

export function upsertStackTemplate(stackTemplates: StackTemplate[], stackTemplate: StackTemplate): StackTemplate[] {
  if (stackTemplates.some((item) => item.id === stackTemplate.id)) {
    return stackTemplates.map((item) => item.id === stackTemplate.id ? stackTemplate : item);
  }
  return [stackTemplate, ...stackTemplates];
}

// planSummaryLabel reads a saved plan's counts the way tofu's summary line
// does, compressed: "+3 ~1 -0" is three to add, one to change, none to destroy.
export function planSummaryLabel(summary: PlanSummary | null): string {
  if (!summary) {
    return "";
  }
  return `+${summary.add} ~${summary.change} -${summary.destroy}`;
}

export interface StackTemplateStatus {
  label: string;
  tone: StatusTone;
  /** What the state means, for the detail panel. The list row shows only the
      icon, so this is where the vocabulary is actually taught. */
  description: string;
}

// stackTemplateStatus answers "is desired state live?" for one row of the list.
// It is deliberately a projection of live_state alone: plan_state answers a
// different question, whether the plan waiting for approval still describes
// desired state, and the approval refuses one that does not.
//
// lifecycle is read first because a template being torn down keeps the
// last_applied_* pointers of the apply that put it live, so live_state would
// report a destroying template as applied.
//
// The tone is returned rather than derived through statusTone(): that helper
// classifies API status strings, and would read "changed" and "destroying" as
// settled.
export function stackTemplateStatus(stackTemplate: StackTemplate): StackTemplateStatus {
  if (stackTemplate.lifecycle === "destroying") {
    return {
      label: "destroying",
      tone: "progress",
      description: "A destroy run is tearing this template's resources down."
    };
  }
  // Only an interrupted destroy sets this lifecycle — a failed apply leaves the
  // template active — so the label says destroy rather than claiming the
  // infrastructure itself failed.
  if (stackTemplate.lifecycle === "failed") {
    return {
      label: "destroy failed",
      tone: "failed",
      description: "A destroy run stopped before it finished. Some resources may still exist."
    };
  }
  switch (stackTemplate.live_state) {
    case "matches":
      // The last apply's intent equals desired. It does not claim reality still
      // matches — that is drift, and it needs a refresh to observe — so this
      // says applied rather than in sync.
      return {
        label: "applied",
        tone: "settled",
        description: "The last apply ran the revision and config that are desired now."
      };
    case "differs":
      // Live infrastructure no longer matches what was asked for, so this is
      // the one row state that wants attention. It keeps the warning tone and
      // "not applied" takes a muted one: a template nobody has applied yet is
      // not a problem to be drawn to.
      return {
        label: "changed",
        tone: "waiting",
        description: "The revision or config changed after the last apply. Plan, then apply, to bring it live."
      };
    default:
      return {
        label: "not applied",
        tone: "canceled",
        description: "Nothing has been applied yet, so this template has no live resources."
      };
  }
}

// hasConfigChanged compares encoded values, so reformatting JSON alone is not
// a change. Text that doesn't parse counts as one: Save stays enabled and the
// click is what shows the error.
function hasConfigChanged(
  stackTemplate: StackTemplate,
  variables: TemplateVariable[],
  variableValues: Record<string, string>
): boolean {
  const current = configFromVariableValues(variables, variableValues);
  if (Object.keys(current.errors).length > 0) {
    return true;
  }
  const saved = configFromVariableValues(variables, variableValuesFromConfig(stackTemplate.config, variables)).config;
  const currentNames = Object.keys(current.config);
  if (currentNames.length !== Object.keys(saved).length) {
    return true;
  }
  return currentNames.some((name) => !(name in saved) || JSON.stringify(current.config[name]) !== JSON.stringify(saved[name]));
}
