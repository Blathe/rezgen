# rezgen

rezgen tailors a resume and cover letter to a specific job posting. It keeps everything about you in one JSON profile, reads a posting, asks a few questions, and uses the Claude API to select and reword the experience that fits. It never invents facts: every line on the page traces back to the profile or your answers.

> Status: Phase 2 of 5. `rezgen validate` and `rezgen generate` (Markdown output) work today; the terminal UI and PDF export are coming next.

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

## Generate an application

rezgen calls the Claude API, so set an API key first (from [console.anthropic.com](https://console.anthropic.com)):

```sh
export ANTHROPIC_API_KEY=sk-ant-...        # PowerShell: $env:ANTHROPIC_API_KEY = "sk-ant-..."
```

Save the job posting as a text file, then:

```sh
rezgen generate -posting posting.txt
```

It runs in three steps:

1. **Analyze.** Claude reads the posting against your profile and lists what the role requires, which requirements your profile already supports, and the gaps.
2. **Ask.** If a true answer could close a gap (say, a tool you've used but never wrote down), rezgen asks you up to 5 questions in the terminal. Press Enter to skip any of them.
3. **Write.** Claude drafts the resume and cover letter. Every bullet and paragraph cites the profile IDs or answers it came from. rezgen rejects a draft that cites an ID that doesn't exist, puts one role's highlight under another role, lists a skill that isn't in your profile, or uses one of your `avoid_words`. It asks once more with the problems listed, and if the second draft fails too it saves that draft as `draft-rejected.json` for you to inspect.

Contact details, job titles, dates, education and certifications are copied straight from the profile, never retyped by the model.

The result lands in `applications/<date>-<company>-<role>/`:

| File | What it is |
| --- | --- |
| `resume.md`, `cover-letter.md` | The tailored documents |
| `sources.md` | Every generated line next to the profile IDs it cites, for checking by hand |
| `analysis.json`, `answers.json`, `draft.json` | The intermediate steps |
| `posting.md` | The posting as given |

Options:

| Flag | Default | |
| --- | --- | --- |
| `-posting` | (required) | Posting file, or `-` to read stdin (questions are skipped) |
| `-profile` | `profile.json` | Profile to draw from |
| `-out` | `applications` | Where application folders go |
| `-model` | `claude-opus-5-5` | Claude model |
| `-effort` | `high` | `low`, `medium`, `high`, `xhigh` or `max`; lower is faster and cheaper |
| `-no-questions` | off | Skip the questions |

Try it on the examples: `rezgen generate -profile examples/profile.example.json -posting examples/posting.example.md`.

A run makes two API calls. The profile is sent as a cached system prompt, so the second call reads it from the cache. Requests use server-side fallbacks: if a safety classifier declines a request, the API retries it on a fallback model within the same call instead of failing.

## Roadmap

1. **Foundation**: profile schema, loader, `rezgen validate` (done)
2. **Headless pipeline**: posting intake, Claude analysis and writing, Markdown output, `rezgen generate` (done)
3. **TUI**: Bubble Tea screens for intake, questions, preview and revision
4. **PDF and tracking**: ATS-friendly PDF export and a saved folder per application
5. **Polish**: recorded-response tests, demo GIF, release binaries

## Development

```sh
go test ./...
go vet ./...
```
