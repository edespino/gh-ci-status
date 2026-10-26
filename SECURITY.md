# Security

## Reporting a problem

Please report security problems privately, through GitHub's private vulnerability reporting for
this repository ("Report a vulnerability" on the Security tab), rather than in a public issue.
Include what you found, how to reproduce it, and what an attacker could do with it.

## What gh-ci-status protects

gh-ci-status reads GitHub with the user's GitHub CLI token and serves a page on the user's
computer. The design keeps both safe:

- The token stays with the GitHub CLI. gh-ci-status reads it with `gh auth token` when it needs
  it, keeps it in memory, never writes, logs or prints it, and only sends it to the GitHub API
  host it was started with (a pagination link to any other host is refused).
- The web server listens on `127.0.0.1` only and refuses requests for any other `Host`.
- Every request must carry a 256-bit random key in its address. There is no cookie, so pages
  from other local servers cannot act through the user's browser.
- Pages load nothing from other sites and set a strict Content-Security-Policy and
  `Referrer-Policy: no-referrer`.
- gh-ci-status never writes to GitHub.
- The Slack webhook for Send to Slack is a secret too. It is accepted only as a
  `https://hooks.slack.com/services/...` address, kept in `slack.json` (readable only by the
  user, refused when others can read it), never shown, logged or put in an error, and posted to
  only when the user presses Send to Slack, without following redirects. That post is the only
  thing gh-ci-status sends anywhere.

If the page address leaks, `gh ci-status serve --new-key` replaces the key.
