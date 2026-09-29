// @vitest-environment jsdom
import { useEffect, useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LogStep, LogSteps } from "./LogSteps";

afterEach(cleanup);

function Steps({ initiallyOpen = {} }: { initiallyOpen?: Record<string, boolean> }) {
  const [open, setOpen] = useState(initiallyOpen);
  return (
    <LogSteps>
      {["init", "plan"].map((name) => (
        <LogStep
          key={name}
          name={name}
          open={open[name] ?? false}
          onToggle={() => setOpen((current) => ({ ...current, [name]: !current[name] }))}
        >
          {`${name} log body`}
        </LogStep>
      ))}
    </LogSteps>
  );
}

describe("LogSteps", () => {
  it("lists one row per step, each a button that says whether its log is open", () => {
    render(<Steps initiallyOpen={{ plan: true }} />);

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "init" }).getAttribute("aria-expanded")).toBe("false");
    const plan = screen.getByRole("button", { name: "plan" });
    expect(plan.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(plan.getAttribute("aria-controls") ?? "")?.textContent).toBe("plan log body");
  });

  // RunLogsPanel passes a component that fetches the log, so a closed row
  // must not mount it.
  it("mounts a log only while its row is open", async () => {
    const mounted = vi.fn();
    function Log() {
      useEffect(() => {
        mounted();
      }, []);
      return <>apply log body</>;
    }
    function OneStep() {
      const [open, setOpen] = useState(false);
      return (
        <LogSteps>
          <LogStep name="apply" open={open} onToggle={() => setOpen(!open)}>
            <Log />
          </LogStep>
        </LogSteps>
      );
    }
    render(<OneStep />);

    expect(mounted).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "apply" }));
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(screen.getByText("apply log body")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "apply" }));
    expect(screen.queryByText("apply log body")).toBeNull();
  });

  it("opens one row without touching the others", async () => {
    render(<Steps initiallyOpen={{ plan: true }} />);

    await userEvent.click(screen.getByRole("button", { name: "init" }));
    expect(screen.getByText("init log body")).toBeTruthy();
    expect(screen.getByText("plan log body")).toBeTruthy();
  });

  // --legacy-touch-target gave these rows 44px on touch screens.
  it("gives each row a 44px target on coarse pointers", () => {
    render(<Steps />);
    for (const button of screen.getAllByRole("button")) {
      expect(button.classList).toContain("pointer-coarse:min-h-11");
    }
  });
});
