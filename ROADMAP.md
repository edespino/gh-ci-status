# Roadmap

What comes next for gh-ci-status, in order. Requests and ideas are welcome as
[issues](https://github.com/edespino/gh-ci-status/issues).

## Next: an MCP server (`gh ci-status mcp`)

A mode that lets AI assistants (any MCP client) read the same status as the web page and the
terminal: the same watch list, the same rules, the same GitHub CLI login.

- The MCP client starts it as `gh ci-status mcp` and talks to it over standard input and
  output.
- Read-only tools first:
  - `status`: the JSON report for everything watched, the same as `gh ci-status --json`.
  - `check`: the setup checks (GitHub CLI, login, API and each repository).
  - `repositories` and `workflows`: what is set up, and what a repository offers.
- Built on the `cistatus` library, with the standard library only (MCP over standard input and
  output is JSON-RPC), so the project keeps no dependencies.
- Text from GitHub (workflow, job and step names, error messages) is returned as data, never
  as instructions to the model.
- It works next to other MCP servers: a client can use these tools alongside its own.
- Changing what is watched (`Config.AddWatches`, `Config.RemoveWatch`) comes later, and only
  with the client's confirmation.

## Later

- Sending to Slack from the terminal (`gh ci-status slack`), for scheduled posts, with the same
  message as the Send to Slack button.
- A theme choice on the pages: system, light and dark.
