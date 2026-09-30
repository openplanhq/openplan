#!/usr/bin/env node
/**
 * Drive the openplan web app through a real OIDC login in headless Chrome,
 * so auth-gated screens can be inspected without a human taking screenshots.
 *
 * Why this exists: every screen except /styleguide needs a session, and the
 * only way to one is the identity provider (Dex on the local stack) — there is
 * no dev bypass. The driver clicks the app's "Sign in" button, fills Dex's
 * password form, and then navigates by clicking through the SPA.
 *
 * Setup:
 *   cd web && npm run dev                       # must be localhost:5173: the
 *                                               # OIDC client only registers
 *                                               # that as a redirect_uri
 *   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
 *     --headless=new --disable-gpu --remote-debugging-port=9222 \
 *     --user-data-dir=/tmp/openplan-chrome --window-size=1512,950 about:blank &
 *
 * Credentials come from the environment; nothing is hardcoded. On the local
 * stack, root is Dex's static user from deploy/dex/config.yaml:
 *   export OPENPLAN_USER=admin@openplan.local
 *   export OPENPLAN_PASS=<its password, in docs/authentication.md>
 *
 * Usage:
 *   node scripts/drive-web.mjs --shot out.png --click "dev" --click "Access"
 *   node scripts/drive-web.mjs --click Templates --probe 'document.title'
 *   node scripts/drive-web.mjs --fields --click "dev" --click Environment
 *   node scripts/drive-web.mjs --fail '*v1/me=500' --reload --shot err.png
 *
 * Flags:
 *   --click <label>   click the <a>/<button> whose text matches (repeatable,
 *                     applied in order, 3.5s settle between each)
 *   --shot <file>     write a full-page PNG
 *   --probe <expr>    evaluate a JS expression in the page and print the result
 *   --fields          print every input/select/textarea width vs its parent,
 *                     which is how field-stretch regressions get caught
 *   --goto <path>     client-side navigate to path (repeatable, applied before
 *                     clicks, 3.5s settle between each)
 *   --fail <glob>=<status>
 *                     answer requests whose URL matches glob with that status
 *                     and the API's JSON error body (repeatable). Takes effect
 *                     after sign-in, so sign-in itself still works. In the
 *                     glob, `*` is any run of characters and `?` is exactly
 *                     one, so a literal `?` can't be matched.
 *   --eval <expr>     evaluate an expression in the page after --goto and
 *                     before --reload (repeatable, 1.5s settle after each)
 *   --reload          reload the page after --eval, so every query runs again
 *                     against --fail
 *   --signed-out      skip signing in; no credentials needed
 *   --port <n>        devtools port (default 9222)
 */
import { writeFileSync } from "node:fs";

const argv = process.argv.slice(2);
const clicks = [], fails = [], evals = [];
let shot = null, probe = null, fields = false, port = 9222;
let gotos = [], signedOut = false, reload = false;
for (let i = 0; i < argv.length; i++) {
  const next = () => argv[++i];
  if (argv[i] === "--click") clicks.push(next());
  else if (argv[i] === "--shot") shot = next();
  else if (argv[i] === "--probe") probe = next();
  else if (argv[i] === "--fields") fields = true;
  else if (argv[i] === "--port") port = Number(next());
  else if (argv[i] === "--goto") gotos.push(next());
  else if (argv[i] === "--signed-out") signedOut = true;
  else if (argv[i] === "--fail") fails.push(next());
  else if (argv[i] === "--eval") evals.push(next());
  else if (argv[i] === "--reload") reload = true;
}

// --fail specs, split at the last "=" so a glob may contain one.
const failures = fails.map((spec) => {
  const at = spec.lastIndexOf("=");
  const glob = spec.slice(0, at);
  const pattern = glob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  return { glob, status: Number(spec.slice(at + 1)), test: new RegExp(`^${pattern}$`) };
});
if (failures.some((f) => !f.glob || !Number.isInteger(f.status))) {
  console.error("--fail takes <glob>=<status>, for example '*/v1/me=500'.");
  process.exit(2);
}

const USER = process.env.OPENPLAN_USER;
const PASS = process.env.OPENPLAN_PASS;
if (!signedOut && (!USER || !PASS)) {
  console.error("OPENPLAN_USER and OPENPLAN_PASS must be set (see the header of this file).");
  process.exit(2);
}

const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json().catch(() => null);
const page = list?.find((t) => t.type === "page");
if (!page) {
  console.error(`No Chrome page target on :${port}. Start Chrome with --remote-debugging-port=${port}.`);
  process.exit(2);
}

const ws = new WebSocket(page.webSocketDebuggerUrl);
let id = 0;
const pending = new Map();
const send = (method, params = {}) =>
  new Promise((resolve, reject) => {
    const msgId = ++id;
    pending.set(msgId, { resolve, reject });
    ws.send(JSON.stringify({ id: msgId, method, params }));
  });
// Chrome pauses each request a --fail glob matches and waits for an answer.
// The body is the API's error shape, so the app's error handling runs as it
// would for a real failure.
const answer = ({ requestId, request }) => {
  const failure = failures.find((f) => f.test.test(request.url));
  if (!failure) return send("Fetch.continueRequest", { requestId });
  const body = JSON.stringify({ error: "injected", message: `drive-web --fail ${failure.glob}` });
  return send("Fetch.fulfillRequest", {
    requestId,
    responseCode: failure.status,
    responseHeaders: [{ name: "Content-Type", value: "application/json" }],
    body: Buffer.from(body).toString("base64")
  });
};
ws.addEventListener("message", (event) => {
  const msg = JSON.parse(event.data);
  if (msg.method === "Fetch.requestPaused") {
    answer(msg.params).catch((error) => console.error("--fail:", error.message));
    return;
  }
  const slot = msg.id && pending.get(msg.id);
  if (!slot) return;
  pending.delete(msg.id);
  msg.error ? slot.reject(new Error(JSON.stringify(msg.error))) : slot.resolve(msg.result);
});
const evaluate = async (expression) =>
  (await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })).result.value;
const settle = (ms) => new Promise((r) => setTimeout(r, ms));

await new Promise((r) => ws.addEventListener("open", r));
await send("Page.enable");
await send("Runtime.enable");
// localhost, not 127.0.0.1: the transaction cookie is set on the origin that
// starts sign-in, and the provider sends the browser back to localhost.
await send("Page.navigate", { url: "http://localhost:5173/stacks" });
await settle(6000);

<<<<<<< HEAD
// Signed out, the app lands on its sign-in screen: one button that leaves for
// the provider.
if (await evaluate("!!document.querySelector('[data-testid=signin-submit]')")) {
  await evaluate("document.querySelector('[data-testid=signin-submit]').click()");
  await settle(4000);
}

// Dex's login page is server-rendered, so setting .value and submitting the
// form is enough — no React synthetic-event plumbing needed.
if (await evaluate("!!document.querySelector('#login')")) {
=======
// The app's own sign-in form submits through React, so .submit() would skip
// its handler; requestSubmit() fires the submit event React listens for.
if (!signedOut && await evaluate("!!document.querySelector('#signin-username')")) {
  await evaluate(`(() => {
    const u = document.querySelector('#signin-username'), p = document.querySelector('#signin-password');
    const set = (el, v) => {
      Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    set(u, ${JSON.stringify(USER)});
    set(p, ${JSON.stringify(PASS)});
    u.form.requestSubmit();
  })()`);
  await settle(8000);
}

// Keycloak's login page is server-rendered, so setting .value and submitting the
// form is enough — no React synthetic-event plumbing needed.
if (!signedOut && await evaluate("!!document.querySelector('#username')")) {
>>>>>>> 121652f (fix(web): restore browser defaults legacy screens relied on under preflight)
  await evaluate(`(() => {
    const u = document.querySelector('#login'), p = document.querySelector('#password');
    const set = (el, v) => {
      Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    set(u, ${JSON.stringify(USER)});
    set(p, ${JSON.stringify(PASS)});
    u.form.submit();
  })()`);
  await settle(8000);
}
console.error("signed in at:", await evaluate("location.pathname"));

// After sign-in, so a --fail on /v1/me doesn't stop the driver signing in.
if (failures.length) {
  await send("Fetch.enable", { patterns: failures.map((f) => ({ urlPattern: f.glob, requestStage: "Request" })) });
}

// Client-side navigation: react-router follows popstate, and a full load
// would drop the in-memory session state the SPA holds.
for (const path of gotos) {
  await evaluate(`history.pushState({}, "", ${JSON.stringify(path)}); dispatchEvent(new PopStateEvent("popstate"))`);
  await settle(3500);
}

for (const expression of evals) {
  console.error("eval:", await evaluate(expression));
  await settle(1500);
}

if (reload) {
  await send("Page.reload");
  // A failing query retries three times, 2 to 3 seconds apart, before its
  // screen gives up and shows the error (queryClient.ts, polling.ts).
  await settle(12000);
}

for (const label of clicks) {
  const result = await evaluate(`(() => {
    const all = Array.from(document.querySelectorAll('a, button'));
    const exact = all.find((x) => x.textContent.trim() === ${JSON.stringify(label)});
    const prefix = all.find((x) => x.textContent.trim().startsWith(${JSON.stringify(label)}));
    const el = exact || prefix;
    if (!el) return 'NOT FOUND: ' + ${JSON.stringify(label)};
    el.click();
    return 'clicked: ' + ${JSON.stringify(label)};
  })()`);
  console.error(result);
  if (result.startsWith("NOT FOUND")) { ws.close(); process.exit(1); }
  await settle(3500);
}
console.error("at:", await evaluate("location.pathname"));

if (fields) {
  console.log(await evaluate(`JSON.stringify(
    Array.from(document.querySelectorAll('input, select, textarea')).map((el) => ({
      tag: el.tagName.toLowerCase(),
      width: Math.round(el.getBoundingClientRect().width),
      parentWidth: Math.round(el.parentElement.getBoundingClientRect().width),
      label: (el.getAttribute('aria-label') || el.placeholder || el.id || '').slice(0, 30)
    })), null, 1)`));
}
if (probe) console.log(await evaluate(probe));
if (shot) {
  const png = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true });
  writeFileSync(shot, Buffer.from(png.data, "base64"));
  console.error("screenshot ->", shot);
}

ws.close();
process.exit(0);
