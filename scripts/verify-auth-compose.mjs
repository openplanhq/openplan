#!/usr/bin/env node

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const rendered = execFileSync(
  "docker",
  [
    "compose",
    "--env-file",
    ".env.example",
    "-f",
    "docker-compose.yaml",
    "config",
    "--format",
    "json",
  ],
  { cwd: root, encoding: "utf8" },
);
const config = JSON.parse(rendered);
const source = readFileSync(resolve(root, "docker-compose.yaml"), "utf8");
const envExample = readFileSync(resolve(root, ".env.example"), "utf8");

function envValue(name) {
  const prefix = `${name}=`;
  const matches = envExample
    .split(/\r?\n/)
    .filter((line) => line.startsWith(prefix));
  assert.equal(matches.length, 1, `${name} must appear exactly once in .env.example`);
  return matches[0].slice(prefix.length);
}

for (const [name, value] of Object.entries({
  OPENPLAN_ENVIRONMENT: "development",
  OPENPLAN_TENANT_ID: "tenant_123",
  OPENPLAN_PUBLIC_URL: "http://localhost:5173",
})) {
  assert.equal(envValue(name), value, `${name} has the wrong local example value`);
}

function service(name) {
  const value = config.services?.[name];
  assert.ok(value, `missing Compose service: ${name}`);
  return value;
}

function hasVolume(value, sourceName) {
  return value.volumes?.some(
    (volume) => volume.type === "volume" && volume.source === sourceName,
  );
}

const postgres = service("postgres");
const api = service("api");

assert.equal(postgres.image, "postgres:16-alpine");
assert.ok(postgres.healthcheck, "the shared Postgres needs a health check");

// OPENPLAN_PUBLIC_URL is what the API derives its OIDC redirect and
// post-logout URIs from, and what a provider must register. A stale Compose
// default would only surface at sign-in as invalid_redirect_uri, so pin every
// "${OPENPLAN_PUBLIC_URL:-...}" default to the .env.example value.
const publicURLDefaults = [
  ...source.matchAll(/\$\{OPENPLAN_PUBLIC_URL:-([^}]*)\}/g),
].map((match) => match[1]);
assert.ok(publicURLDefaults.length > 0, "expected OPENPLAN_PUBLIC_URL to default in the api service");
for (const value of publicURLDefaults) {
  assert.equal(
    value,
    envValue("OPENPLAN_PUBLIC_URL"),
    "every OPENPLAN_PUBLIC_URL default in Compose must match .env.example",
  );
}
assert.equal(api.environment?.OPENPLAN_PUBLIC_URL, envValue("OPENPLAN_PUBLIC_URL"));

// The three OIDC settings move together and are empty by default: with no
// issuer the API serves local accounts, so the default stack needs no IdP.
for (const name of ["OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET"]) {
  assert.match(source, new RegExp(`\\$\\{${name}:-\\}`), `${name} must default to empty`);
}

// OpenFGA is embedded in the API. A service, a provisioner, or a required
// store or model identifier coming back would reintroduce the two-phase
// startup the single Compose file exists to remove.
assert.deepEqual(
  Object.keys(config.services).filter((name) => name.includes("openfga")),
  [],
  "OpenFGA runs inside the API; Compose must not start it as a service",
);
assert.doesNotMatch(
  source,
  /OPENFGA_(STORE_ID|MODEL_ID|API_URL|API_TOKEN)/,
  "the API resolves its OpenFGA store and model in process; Compose must not pass identifiers or a URL",
);

assert.ok(hasVolume(postgres, "postgres-data"));
assert.ok(config.volumes?.["postgres-data"]);

assert.match(
  source,
  /\$\{SESSION_ENCRYPTION_KEY:-/,
  "SESSION_ENCRYPTION_KEY must have an inline default so the stack runs without .env",
);

console.log("authentication Compose contract verified");
