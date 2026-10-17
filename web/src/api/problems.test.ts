import { describe, expect, it } from "vitest";
import { ApiRequestError } from "./client";
import { apiErrorMessage, fieldErrors } from "./problems";
import type { ApiProblem } from "./types";

function problem(overrides: Partial<ApiProblem>): ApiRequestError {
  return new ApiRequestError({
    type: "https://openplan.run/problems/validation-failed",
    title: "The request has invalid fields",
    status: 422,
    ...overrides
  });
}

describe("fieldErrors", () => {
  it("keys errors under a prefix by the rest of the pointer", () => {
    const error = problem({
      errors: [
        { pointer: "#/config/region", issue: "required", detail: "region is required" },
        { pointer: "#/name", issue: "required", detail: "name is required" }
      ]
    });

    expect(fieldErrors(error, "#/config/")).toEqual({ region: "region is required" });
    expect(fieldErrors(error, "#/")).toEqual({ "config/region": "region is required", name: "name is required" });
  });

  // A variable name is the user's, so the server escapes it; the form must
  // find the same name it drew.
  it("undoes the pointer's URI and RFC 6901 escapes", () => {
    const error = problem({ errors: [{ pointer: "#/config/a~1b~0c%20d", issue: "unknown", detail: "a/b~c d is not a variable" }] });

    expect(fieldErrors(error, "#/config/")).toEqual({ "a/b~c d": "a/b~c d is not a variable" });
  });

  it("ignores errors that aren't API problems", () => {
    expect(fieldErrors(new Error("boom"), "#/")).toEqual({});
  });
});

describe("apiErrorMessage", () => {
  it("lists only the field errors the form doesn't draw inline", () => {
    const error = problem({
      detail: "2 fields need attention.",
      errors: [
        { pointer: "#/config/region", issue: "required", detail: "region is required" },
        { pointer: "#/template_revision_id", issue: "required", detail: "template_revision_id is required" }
      ]
    });

    expect(apiErrorMessage(error, "Request failed", "#/config/")).toBe("template_revision_id is required");
    expect(apiErrorMessage(error, "Request failed")).toBe("region is required; template_revision_id is required");
  });

  it("is empty when every field error is drawn inline", () => {
    const error = problem({ errors: [{ pointer: "#/config/region", issue: "required", detail: "region is required" }] });

    expect(apiErrorMessage(error, "Request failed", "#/config/")).toBe("");
  });

  it("names the request id for a server fault", () => {
    const error = new ApiRequestError({
      type: "about:blank",
      title: "Internal Server Error",
      status: 500,
      detail: "Something went wrong on the server. Quote the request ID when you report it.",
      request_id: "abc123"
    });

    expect(apiErrorMessage(error, "Request failed")).toBe(
      "Something went wrong on the server. Quote the request ID when you report it. Request ID: abc123."
    );
  });

  it("uses the detail of a conflict, and the fallback for anything else", () => {
    const conflict = new ApiRequestError({
      type: "https://openplan.run/problems/last-owner",
      title: "The stack needs an owner",
      status: 409,
      detail: "This is the stack's last owner. Make someone else owner first."
    });

    expect(apiErrorMessage(conflict, "Request failed")).toBe("This is the stack's last owner. Make someone else owner first.");
    expect(apiErrorMessage("not an error", "Request failed")).toBe("Request failed");
  });
});
