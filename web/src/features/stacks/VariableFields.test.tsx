// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TemplateVariable } from "../../api/types";
import VariableFields from "./VariableFields";

function variable(overrides: Partial<TemplateVariable> = {}): TemplateVariable {
  return {
    template_revision_id: "rev_1",
    name: "region",
    type_expression: "string",
    description: "",
    required: false,
    has_default: false,
    sensitive: false,
    has_validation: false,
    ...overrides
  };
}

function renderFields(variables: TemplateVariable[], props: Partial<React.ComponentProps<typeof VariableFields>> = {}) {
  const onVariableValueChange = vi.fn();
  render(
    <MemoryRouter>
      <VariableFields
        variables={variables}
        variableValues={{}}
        onVariableValueChange={onVariableValueChange}
        emptyMessage="No variables."
        environmentPath="/stacks/s1/templates/t1/environment"
        {...props}
      />
    </MemoryRouter>
  );
  return { onVariableValueChange };
}

// The ids aria-describedby names, resolved to their text, in order.
function describedBy(field: HTMLElement): string[] {
  return (field.getAttribute("aria-describedby") ?? "")
    .split(" ")
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? `missing #${id}`);
}

describe("VariableFields", () => {
  afterEach(cleanup);

  // A sensitive value can't go in config, which is stored in the clear, so
  // the form has no box for it: it names the secret that sets it instead.
  it("draws a sensitive variable without an input, naming its secret", () => {
    renderFields([variable({ name: "db_password", sensitive: true, required: true }), variable({ name: "region" })]);

    expect(screen.queryByLabelText(/db_password/)).toBeNull();
    expect(screen.getByLabelText("region")).toBeTruthy();
    expect(screen.getByText("TF_VAR_db_password")).toBeTruthy();
    expect(screen.getByText("Sensitive")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Environment" }).getAttribute("href")).toBe("/stacks/s1/templates/t1/environment");
  });

  it.each(["string", "number", "bool"])("keeps a one-line input for %s, its type as the placeholder only", (type) => {
    renderFields([variable({ name: "v", type_expression: type })]);

    const field = screen.getByLabelText("v");
    expect(field.tagName).toBe("INPUT");
    expect(field.getAttribute("placeholder")).toBe(type);
    expect(field.getAttribute("aria-describedby")).toBeNull();
  });

  it.each(["list(string)", "map(string)", "set(string)", "tuple([string, number])", "any"])(
    "gives %s a textarea with the type shown above it and named by the field",
    (type) => {
      renderFields([variable({ name: "v", type_expression: type })]);

      const field = screen.getByLabelText("v");
      expect(field.tagName).toBe("TEXTAREA");
      expect(field.getAttribute("placeholder")).toBeNull();
      expect(describedBy(field)).toEqual([type]);
      // Shown, not only announced: the type sits before the field in the page.
      const shown = document.getElementById(field.getAttribute("aria-describedby")!)!;
      expect(shown.compareDocumentPosition(field) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
  );

  it("says any for a variable with no type", () => {
    renderFields([variable({ name: "v", type_expression: "" })]);

    const field = screen.getByLabelText("v");
    expect(field.tagName).toBe("TEXTAREA");
    expect(describedBy(field)).toEqual(["any"]);
  });

  it("keeps a multi-line object type as written, line breaks and indentation included", () => {
    const type = "object({\n    cidr = string\n    azs  = list(string)\n  })";
    renderFields([variable({ name: "network", type_expression: type })]);

    const field = screen.getByLabelText("network");
    const shown = document.getElementById(field.getAttribute("aria-describedby")!)!;
    expect(shown.tagName).toBe("PRE");
    expect(shown.textContent).toBe(type);
  });

  it("reports edits to a textarea by variable name", () => {
    const { onVariableValueChange } = renderFields([variable({ name: "tags", type_expression: "map(string)" })]);

    fireEvent.change(screen.getByLabelText("tags"), { target: { value: '{"team": "core"}' } });

    expect(onVariableValueChange).toHaveBeenCalledWith("tags", '{"team": "core"}');
  });

  it("names the error, then the type, then the description, and marks the textarea invalid", () => {
    renderFields([variable({ name: "tags", type_expression: "map(string)", description: "Tags for every resource." })], {
      errors: { tags: "tags is not valid JSON" }
    });

    const field = screen.getByLabelText("tags");
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(describedBy(field)).toEqual(["tags is not valid JSON", "map(string)", "Tags for every resource."]);
  });

  it("turns off spelling marks and autocapitalisation in a textarea", () => {
    renderFields([variable({ name: "azs", type_expression: "list(string)" })]);

    const field = screen.getByLabelText("azs");
    expect(field.getAttribute("spellcheck")).toBe("false");
    expect(field.getAttribute("autocapitalize")).toBe("off");
  });

  it("locks a textarea with the rest of the fields", () => {
    renderFields([variable({ name: "azs", type_expression: "list(string)" })], { disabled: true });

    expect((screen.getByLabelText("azs") as HTMLTextAreaElement).disabled).toBe(true);
  });
});
