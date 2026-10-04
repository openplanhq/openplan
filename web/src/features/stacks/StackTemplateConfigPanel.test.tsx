// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TemplateVariable } from "../../api/types";
import StackTemplateConfigPanel from "./StackTemplateConfigPanel";

function variable(overrides: Partial<TemplateVariable> = {}): TemplateVariable {
  return {
    template_revision_id: "rev_1",
    name: "region",
    type_expression: "string",
    description: "",
    required: true,
    has_default: false,
    sensitive: false,
    has_validation: false,
    ...overrides
  };
}

function renderPanel(overrides: Partial<React.ComponentProps<typeof StackTemplateConfigPanel>> = {}) {
  const props = {
    variables: [variable()],
    variableValues: { region: "us-east-1" },
    onVariableValueChange: vi.fn(),
    canSave: true,
    onSave: vi.fn(),
    saveBusy: false,
    ...overrides
  };
  render(<StackTemplateConfigPanel {...props} />);
  return props;
}

describe("StackTemplateConfigPanel", () => {
  afterEach(cleanup);

  it("offers exactly one action, Save variables", () => {
    renderPanel();

    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Save variables" })).toBeTruthy();
  });

  it("reports edits to a variable value", () => {
    const props = renderPanel();

    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });

    expect(props.onVariableValueChange).toHaveBeenCalledWith("region", "eu-west-1");
  });

  it("disables save when canSave is false", () => {
    renderPanel({ canSave: false });

    expect((screen.getByRole("button", { name: "Save variables" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("puts the reason above the fields, and locks every input and the action", () => {
    renderPanel({ disabledReason: "Editing requires operator access." });

    const reason = screen.getByTestId("variables-disabled-reason");
    expect(reason.textContent).toBe("Editing requires operator access.");
    const input = screen.getByLabelText(/region/) as HTMLInputElement;
    expect(Boolean(reason.compareDocumentPosition(input) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    expect(input.disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Save variables" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("names a required variable with an asterisk and describes it under its input", () => {
    renderPanel({ variables: [variable({ description: "Region to deploy into." })] });

    const input = screen.getByLabelText("region *");
    const describedBy = input.getAttribute("aria-describedby") ?? "";
    expect(document.getElementById(describedBy)?.textContent).toBe("Region to deploy into.");
  });

  it("describes nothing for a variable without a description", () => {
    renderPanel();

    expect(screen.getByLabelText("region *").getAttribute("aria-describedby")).toBeNull();
  });

  it("says so when the template declares no variables", () => {
    renderPanel({ variables: [], variableValues: {} });

    expect(screen.getByText("This template declares no variables.")).toBeTruthy();
  });
});
