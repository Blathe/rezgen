# rezgen

rezgen tailors a resume and cover letter to a specific job posting. It keeps everything about you in one profile, reads a posting, asks a few questions, and uses the Claude API to select and reword the experience that fits. It never invents facts: every line on the page traces back to your profile or your answers.

> Status: Phases 1-4 of 5. The interactive app, generation with PDF output and the command-line tools all work today.

## Quick start

```sh
go install github.com/Blathe/rezgen/cmd/rezgen@latest
rezgen
```

The first launch walks you through setup:

1. **API key.** Paste a key from [console.anthropic.com](https://console.anthropic.com) (Settings > API keys). rezgen checks it works before going on. (OpenAI support is coming next.)
2. **Model.** Claude Opus 5.5 writes best; Sonnet 5.5 is faster and about half the cost; Haiku 4.5 is the cheapest.
3. **Data folder.** Where your profile and applications live, `Documents/rezgen` by default.
4. **Import.** If you ran an earlier version of rezgen from the folder you're in, it offers to copy your `.env` key, `profile.json` and `applications/` across.
5. **Profile.** If you don't have one yet, paste your resume or LinkedIn profile and Claude drafts it, listing anything worth adding (metrics, missing dates). Or point it at an existing `profile.json`.

Settings are saved to `%AppData%\rezgen\config.json` on Windows, `~/Library/Application Support/rezgen/config.json` on macOS and `~/.config/rezgen/config.json` on Linux, readable only by you. Press `,` in the app to change the model or key later.

## Using the app

```
  rezgen  resume and cover letter tailoring

    STATUS       CREATED              NAME
  ▸ not started  2026-10-03 (today)   Acme - Automation Engineer
    generated    2026-10-02 (1 day)   Northwind Freight - AI Solutions Engineer

  2 applications: 1 generated, 1 not started

  ↑/↓ move · enter open · n new application · d delete · , settings · q quit
```

**Add an application** with `n`: paste the job posting's link, or copy the whole description from the page and paste it (finish with `ctrl+s`). rezgen saves the posting right away, since postings disappear when jobs close, and suggests a name you can edit. The application starts as **not started**.

**Open it** with `enter` and press `g` to generate. A checklist shows each step as it runs:

1. **Analyze.** Claude reads the posting against your profile: what the role requires, which requirements you already meet, and the gaps.
2. **Questions.** If a true answer could close a gap (say, a tool you've used but never wrote down), you get up to 5 questions. Enter on an empty answer skips one; `esc` skips the rest. Your answers can be saved to the profile's `learned_facts`, so no later application asks again.
3. **Write.** Claude drafts the resume and cover letter. Every line cites the profile entries or answers it came from, and rezgen rejects a draft that cites anything that doesn't exist, puts one job's accomplishment under another, lists a skill you don't have, or uses one of your `avoid_words`. It retries once with the problems listed.
4. **PDFs.** Plain on purpose so applicant tracking systems can read them: one column, selectable Helvetica text, no tables or images.

When it's done the application shows **generated**, with clickable links to the PDFs, the folder and the posting. Then:

| Key | Does |
| --- | --- |
| `o` / `c` | Open the resume / cover letter PDF |
| `e` / `l` | Edit the resume / cover letter in your editor; the PDF is rebuilt when you close it |
| `v` | Show where each generated line came from |
| `g` | Regenerate (replaces the current documents) |
| `t` / `p` / `f` | View the saved posting / open its link / open the documents folder |
| `d` | Delete the application |
| `esc` | Back to the list (also cancels a running request) |

Contact details, job titles, dates, education and certifications are always copied straight from the profile, never retyped by the model. A generation makes two API calls; the profile is sent as a cached system prompt, so the second call reads it from the cache, and server-side fallbacks retry on another model if a safety classifier declines a request.

## Your profile

The profile is everything rezgen knows about you. Write it as a superset of any one resume: each highlight is a factual accomplishment with its own `id`, plus `metrics`, `skills` and `tags` so the right ones can be picked for each posting. Setup can draft it for you; [`examples/profile.example.json`](examples/profile.example.json) shows the format.

| Section | What goes in it |
| --- | --- |
| `contact` | Name, email, phone, location, links |
| `headline_variants` | Titles you'd use at the top, e.g. "AI Solutions Engineer" |
| `summary_facts` | Short facts the summary can draw from |
| `experience` | Roles with `start`/`end` as `YYYY-MM` (`end: null` if current) and highlights |
| `projects` | Side projects and open source |
| `skills` | Named groups of skills, e.g. `languages`, `ai` |
| `education`, `certifications` | Credentials |
| `cover_letter_stories` | Short paragraphs about motivation or notable wins (used only in cover letters) |
| `preferences` | Tone, page limit, words to avoid |
| `learned_facts` | Your saved answers to rezgen's questions (edit or delete freely) |

The full JSON Schema is [`internal/profile/profile.schema.json`](internal/profile/profile.schema.json) (also printed by `rezgen schema`); point your editor at it with a `"$schema"` key for completion and inline errors. `rezgen validate` checks the schema plus rules a schema can't: every `id` is unique and no role ends before it starts.

## Command line

Everything the app does is also available as subcommands, for scripting. They use the settings from setup unless you pass flags; without settings they fall back to `./profile.json`, `./applications` and an `ANTHROPIC_API_KEY` from the environment or a `.env` file.

```sh
rezgen generate -posting https://job-boards.greenhouse.io/acme/jobs/123
rezgen generate -posting posting.txt -no-questions
rezgen posting <url>          # show the text rezgen extracts from a page, without calling the API
rezgen pdf path/to/file.md    # turn any Markdown file into a PDF in the same style
rezgen list                   # list applications
rezgen validate               # check the profile
rezgen ui -model claude-sonnet-5-5   # open the app with one-off overrides
```

`generate` takes `-profile`, `-out`, `-model`, `-effort` (`low` to `max`), `-no-questions`, `-no-pdf` and `-env`. For links, rezgen uses the structured job data most boards embed (Greenhouse, Lever, Ashby and many career sites) and otherwise the page's visible text. Sites that build pages with JavaScript or block automated requests, such as LinkedIn and Workday, get a clear error: paste the text instead.

## Where things are kept

```
Documents/rezgen/
  profile.json
  applications/
    2026-10-03-142530-acme-automation-engineer/
      resume.pdf
      cover-letter.pdf
  .state/
    2026-10-03-142530-acme-automation-engineer.json
```

Each application's folder holds only its two PDFs. Everything else rezgen needs (the saved posting, the analysis, the generated draft, your answers and any hand edits) is in one JSON file per application under `.state/`, named with the time it was created so names never collide. You don't need to open it; files are written safely (to a temporary file, then renamed), so a crash can't corrupt them.

Applications made by earlier versions kept all of this inside their folders. The app offers to convert them on launch: each folder is left with just its PDFs (built first if missing), and your documents stay exactly as they were.

## Roadmap

1. **Foundation**: profile schema, loader, `rezgen validate` (done)
2. **Headless pipeline**: posting intake, Claude analysis and writing, `rezgen generate` (done)
3. **Interactive app**: setup, applications, generation (done); revising a draft with feedback next
4. **PDF export** (done)
5. **Polish**: OpenAI support, recorded-response tests, demo GIF, release binaries

## Development

```sh
go test ./...
go vet ./...
```

To check how the app's screens look without a terminal, render them in full color to an HTML page:

```sh
REZGEN_PREVIEW=preview.html go test ./internal/tui -run TestPreview
```
