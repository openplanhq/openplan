import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import ServiceUnavailable from "./ServiceUnavailable";

describe("ServiceUnavailable", () => {
  // A 503 is missing server setup (an encryption key, a queue), which only
  // an operator can fix, so the page sends the person to one.
  it("renders the service-unavailable banner", () => {
    const markup = renderToStaticMarkup(<ServiceUnavailable />);

    expect(markup).toContain('data-testid="route-service-unavailable"');
    expect(markup).toContain('data-slot="empty"');
    expect(markup).toContain("Service unavailable");
    expect(markup).toContain("Contact your operator.");
  });
});
