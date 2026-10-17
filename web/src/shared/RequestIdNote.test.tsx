// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ApiRequestError } from "../api/client";
import RequestIdNote from "./RequestIdNote";

function apiError(status: number, requestId = "abc123"): ApiRequestError {
  return new ApiRequestError({ type: "about:blank", title: "x", status, request_id: requestId });
}

describe("RequestIdNote", () => {
  afterEach(cleanup);

  it("shows the request id of a server fault", () => {
    render(<RequestIdNote error={apiError(500)} />);
    expect(screen.getByTestId("request-id").textContent).toBe("Request ID: abc123");
  });

  it("shows nothing for a client error, a missing id or a non-API error", () => {
    const { container: notFound } = render(<RequestIdNote error={apiError(404)} />);
    const { container: noID } = render(<RequestIdNote error={apiError(502, "")} />);
    const { container: plain } = render(<RequestIdNote error={new Error("network")} />);
    expect(notFound.innerHTML + noID.innerHTML + plain.innerHTML).toBe("");
  });
});
