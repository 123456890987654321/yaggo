# wiki/

GitHub-Wiki-style documentation for yaggo.

This folder uses the conventions of a GitHub Wiki repository so the files can be cloned into `<repo>.wiki.git` verbatim, or browsed in-tree on GitHub.

## Conventions

- **Flat layout.** No subdirectories — GitHub wikis don't traverse them. Spaces in titles become dashes in filenames.
- **`_Sidebar.md`** is the navigation bar that GitHub renders on every wiki page. (The underscore prefix is the GitHub Wiki convention for "non-content" pages.)
- **Cross-links** use `[Title](Page-Name)` without the `.md` extension — this is how GitHub Wiki resolves them.
- **Entry point** is `Home.md`.

## Files

| File | Purpose |
|------|---------|
| `Home.md` | Landing page: overview, quick start, full navigation. |
| `Installation.md` | `go install`, `go run`, `go:generate`, `tools.go`. |
| `CLI-Reference.md` | Flags, exit codes, example invocations. |
| `Generated-Types.md` | `types.go` — schemas → Go types, nullability, enums, allOf. |
| `Generated-Server.md` | `server.go` — `ServerInterface`, chi, middleware, error handling, response writers. |
| `Generated-Client.md` | `client.go` — `NewClient`, options, content-type negotiation, buffer pool, body draining. |
| `Authentication.md` | `auth.go` — `SecretString`, generic helpers, spec-driven wrappers, security posture. |
| `OpenAPI-Coverage.md` | 3.0/3.1 feature matrix, content types, limitations. |
| `Examples.md` | The `examples/` directory and how to run it. |
| `Development.md` | Building, testing, Makefile, contribution loop. |
| `_Sidebar.md` | Navigation. |

## Publishing to GitHub Wiki

Once the upstream wiki is enabled, sync with:

```sh
git clone https://github.com/123456890987654321/yaggo.wiki.git /tmp/yaggo-wiki
cp wiki/*.md /tmp/yaggo-wiki/
cd /tmp/yaggo-wiki && git add . && git commit -m "Sync from main repo" && git push
```

(Or set up a CI workflow that does the same on every push to `main`.)
