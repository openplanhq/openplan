import { useSearchParams } from "react-router-dom";
import { LogIn } from "lucide-react";
import { ssoLoginURL } from "../api/client";
import { clearLoginAttempts, loginLoopDetected, recordLoginAttempt } from "./loginAttempts";

/** Where a sign-in with no return_to lands. The index route redirects onward. */
const defaultReturnTo = "/";

// Any base works: the question is whether the string stays relative to
// whatever it is resolved against, and a protocol-relative one escapes every
// base equally. Using a fixed one keeps the check independent of where the app
// happens to be served from.
const relativeBase = "https://return-to.invalid";

// return_to decides where the browser goes with a fresh session, so it is an
// open redirect until it is bounded. Only an in-app path is allowed, and
// nothing under /v1, which is the API — /v1/auth/login in particular would
// restart sign-in the moment it succeeded.
//
// The leading slash is not enough on its own to keep a path on this origin.
// `//host` is protocol-relative, and so is `/\host`: the URL parser folds a
// backslash into a slash for http(s), so the browser reads it as a host too.
// Rather than enumerate the spellings, this resolves the string with the same
// parser location.assign() will use and asks where it landed — an origin that
// survives that round trip cannot be talked out of the app.
//
// The server applies the same rule to the copy it receives (authn.SafeReturnTo).
// Both check, so neither side has to trust the other to have done it.
export function safeReturnTo(raw: string | null): string {
  if (!raw || !raw.startsWith("/")) return defaultReturnTo;
  // The parser drops control characters before it decides what the string
  // means, so a value carrying them navigates somewhere other than the value
  // that was checked. Refuse them rather than validate one string and hand
  // location.assign() a different one.
  if (/[\u0000-\u001f\u007f]/.test(raw)) return defaultReturnTo;

  let parsed: URL;
  try {
    parsed = new URL(raw, relativeBase);
  } catch {
    return defaultReturnTo;
  }
  if (parsed.origin !== relativeBase) return defaultReturnTo;

  // A traversal segment makes the path something other than it looks like, and
  // the /v1 test below reads the path as written. `/stacks/../v1/auth/login` is
  // the case that joins the two.
  const rawPath = raw.split(/[?#]/, 1)[0];
  if (rawPath.split("/").some((segment) => segment === "." || segment === "..")) {
    return defaultReturnTo;
  }

  if (parsed.pathname === "/v1" || parsed.pathname.startsWith("/v1/")) return defaultReturnTo;
  return raw;
}

/**
 * The sign-in screen: one button that hands the browser to the identity
 * provider. openplan asks for no credential itself, so there is nothing to
 * type here and nothing to ask the server before rendering.
 *
 * It sits outside SessionProvider: everything under "/" needs a session to
 * render, and this is the screen you see because you do not have one. It is a
 * button rather than an automatic redirect so that landing here never spends a
 * trip to the provider on its own.
 */
export default function SignInScreen() {
  const [searchParams] = useSearchParams();
  const returnTo = safeReturnTo(searchParams.get("return_to"));

  // Reached only after sign-ins that completed at the provider and that the
  // browser then failed to remember. A spent count means every trip came back
  // with a session and the cookie never survived it.
  const cookiesBlocked = loginLoopDetected();

  const handleSignIn = () => {
    recordLoginAttempt();
    globalThis.location.assign(ssoLoginURL(returnTo));
  };

  const handleRetry = () => {
    clearLoginAttempts();
    globalThis.location.reload();
  };

  if (cookiesBlocked) {
    return (
      <main className="signin-page">
        <section className="panel signin-card" data-testid="signin-cookies-blocked">
          <h1 className="signin-title">We could not keep you signed in</h1>
          <p className="muted signin-lede">
            Sign-in worked, but this browser did not hold on to the session, so every page load
            started it over. That happens when cookies are blocked for this site — by browser
            settings, an extension, or a privacy mode that clears them between page loads.
          </p>
          <p className="muted signin-lede">
            Allow cookies for this site and try again. If it keeps failing, contact your
            administrator.
          </p>
          <button type="button" className="primary-button signin-submit" onClick={handleRetry} data-testid="signin-cookies-retry">
            Try again
          </button>
        </section>
      </main>
    );
  }

  return (
    <main className="signin-page">
      <section className="panel signin-card">
        <header className="signin-header">
          <p className="signin-wordmark">openplan</p>
          <h1 className="signin-title">Sign in</h1>
        </header>

        <button type="button" className="primary-button signin-submit" onClick={handleSignIn} data-testid="signin-submit">
          <LogIn size={16} />
          Sign in
        </button>
      </section>
    </main>
  );
}
