# rezgen

rezgen tailors a resume and cover letter to a specific job posting. It keeps everything about you in one JSON profile, reads a posting, asks a few questions, and uses the Claude API to select and reword the experience that fits. It never invents facts: every line on the page traces back to the profile or your answers.

> Status: Phase 1 of 5. The profile format and `rezgen validate` work today; generation, the terminal UI and PDF export are coming next.

## Install

```sh
go install github.com/Blathe/rezgen/cmd/rezgen@latest
```

## Your profile

Copy the example and replace it with your own history:

```sh
cp examples/profile.example.json profile.json
rezgen validate            # checks ./profile.json
rezgen validate -profile path/to/profile.json
```

`profile.json` is git-ignored so your details stay local.

Write the profile as a superset of any one resume. Each highlight is a factual accomplishment with its own `id`, plus `metrics`, `skills` and `tags` so the right ones can be picked for each posting.

| Section | What goes in it |
| --- | --- |
| `contact` | Name, email, phone, location, links |
| `headline_variants` | Titles you'd use at the top, e.g. "AI Solutions Engineer" |
| `summary_facts` | Short facts the summary can draw from |
| `experience` | Roles with `start`/`end` as `YYYY-MM` (`end: null` if current) and highlights |
| `projects` | Side projects and open source |
| `skills` | Named groups of skills, e.g. `languages`, `ai` |
| `education`, `certifications` | Credentials |
| `cover_letter_stories` | Short paragraphs about motivation or notable wins |
| `preferences` | Tone, page limit, words to avoid |

The full JSON Schema lives at [`internal/profile/profile.schema.json`](internal/profile/profile.schema.json) (also printed by `rezgen schema`). Point your editor at it with a `"$schema"` key, as the example does, to get completion and inline errors.

`rezgen validate` checks the schema plus rules a schema can't: every `id` is unique across the profile and no role ends before it starts.

## Roadmap

1. **Foundation**: profile schema, loader, `rezgen validate` (done)
2. **Headless pipeline**: posting intake, Claude analysis and writing, Markdown output, `rezgen generate`
3. **TUI**: Bubble Tea screens for intake, questions, preview and revision
4. **PDF and tracking**: ATS-friendly PDF export and a saved folder per application
5. **Polish**: recorded-response tests, demo GIF, release binaries

## Development

```sh
go test ./...
go vet ./...
```
