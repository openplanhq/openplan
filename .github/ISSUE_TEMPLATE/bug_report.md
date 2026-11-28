---
name: Bug report
about: Something in openplan does not behave the way it says it does
title: ""
labels: bug
assignees: ""
---

<!--
A good bug report here is a reproduction. The fastest ones come with the exact
commands, the exact output, and the commit you were on.

If you cannot reproduce it, say so and describe what you saw. "I could not
reproduce, may have been a transient" is a useful report; a guess dressed as a
diagnosis wastes everyone's time.

Never paste credentials, tokens, cloud keys, or real `.env` contents. See
Security & Configuration in AGENTS.md -- provisioning output in particular can
contain sensitive identifiers.
-->

## What happened

<!-- What you did, what you expected instead, and what you got. -->

## Steps to reproduce

<!--
Exact commands, not descriptions. If the reproduction needs the local stack:

    docker compose up -d
    go run ./cmd/api

The web UI starts with `npm run dev` from `web/`.
-->

1.
2.
3.

## What you expected

## Actual output

<!--
The error, the stack trace, the failed run's logs. Trim to what matters, but do
not paraphrase the error message -- the wording is often the whole clue.
-->

```

```

## Environment

<!-- Delete the lines that do not apply. -->

- openplan commit:
- Go version (`go version`):
- Node version (`node --version`, if you touched the UI):
- Terraform or OpenTofu version, if the report is about a run:
- Browser and OS, if the report is about the UI:
- Docker Compose, if you used the local stack:

## Logs

<!--
Run logs from the stack's Runs tab, or the API's own logs. Strip anything
sensitive before pasting.
-->
