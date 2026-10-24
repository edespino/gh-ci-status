# Security

## Reporting a problem

Please report security problems privately, through GitHub's private vulnerability reporting for
this repository ("Report a vulnerability" on the Security tab), rather than in a public issue.
Include what you found, how to reproduce it, and what an attacker could do with it.

## What ci-status protects

ci-status reads GitHub with the user's GitHub CLI token and serves a page on the user's
computer. The design keeps both safe:

- The token stays with the GitHub CLI. ci-status reads it with `gh auth token` when it needs
  it, keeps it in memory, never writes, logs or prints it, and only sends it to the GitHub API
  host it was started with (a pagination link to any other host is refused).
- The web server listens on `127.0.0.1` only and refuses requests for any other `Host`.
- Every request must carry a 256-bit random key in its address. There is no cookie, so pages
  from other local servers cannot act through the user's browser.
- Pages load nothing from other sites and set a strict Content-Security-Policy and
  `Referrer-Policy: no-referrer`.
- ci-status never writes to GitHub.

If the page address leaks, `ci-status serve --new-key` replaces the key.
