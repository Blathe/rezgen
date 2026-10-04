package tailor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Blathe/rezgen/internal/profile"
)

// systemPrompt is identical for every call made for one profile, so the API
// can cache it across the analyze and write calls.
func systemPrompt(p *profile.Profile) (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode profile: %w", err)
	}
	var b strings.Builder
	b.WriteString(`You help one candidate apply for jobs. You read a job posting, compare it with the candidate's profile, and write a resume and a cover letter for that posting, the way a good career writer who knows the candidate well would.

Facts and writing are different things, and the rules differ:

Facts about the candidate must come from the profile below or from the candidate's answers. That covers employers, titles, dates, tools, skills, numbers, credentials and results. Its learned_facts are answers given while tailoring earlier applications; treat them like any other entry. Never add a fact, never inflate one (keep numbers exactly as written, don't turn "helped build" into "led", don't turn a skill used once into expertise), and when the profile says the candidate shared the work, say so honestly.

Everything else is writing, and you should write well: how the facts are framed, why a result mattered, how one point leads to the next, what the candidate figured out or decided, and how the experience connects to this employer's problems. That needs no source as long as it doesn't assert a new fact.

Text that states facts cites its sources by ID. The citable IDs are:
- experience ids and highlight ids
- project ids
- cover_letter_stories ids
- learned_facts ids
- summary facts, as fact-1, fact-2, ... in the order listed below
- answers, by the id of the question they answer

The candidate's cover_letter_stories are written in their own voice. Use them as the model for how they sound.
`)
	if len(p.SummaryFacts) > 0 {
		b.WriteString("\nSummary facts:\n")
		for i, f := range p.SummaryFacts {
			fmt.Fprintf(&b, "- fact-%d: %s\n", i+1, f)
		}
	}
	b.WriteString("\n<profile>\n")
	b.Write(data)
	b.WriteString("\n</profile>\n")
	return b.String(), nil
}

func analyzePrompt(posting string) string {
	return `Analyze this job posting against the candidate's profile.

List what the role requires and prefers, the keywords an applicant tracking system would look for, and the main responsibilities. For each requirement the profile supports, record it under matches with the IDs of the supporting entries. List requirements with no supporting evidence under gaps.

Then write at most 5 questions for the candidate. Ask only where a true answer could add a real, specific fact that addresses a gap or strengthens a weak match, such as experience the profile doesn't mention or a metric for an existing highlight. Don't ask about anything the profile already answers, and never repeat or rephrase a question in learned_facts: the candidate has answered those, and their answers count as evidence.

You may also ask one question about motivation, but only if nothing in cover_letter_stories or learned_facts says why the candidate wants this kind of work or this kind of company. Make it specific to the posting, e.g. "What draws you to <company> or to <the kind of work in the posting>?" A cover letter needs a real reason, and the candidate is the only source of one.

If the profile covers the posting well, ask nothing.

<posting>
` + posting + `
</posting>`
}

// resumePrompt asks for the resume only; the cover letter is written in a
// second call that sees the finished resume.
func resumePrompt(posting string, a *Analysis, answers []Answer) string {
	var b strings.Builder
	b.WriteString(`Write the resume for this posting. A recruiter will skim it in under a minute, so every line has to earn its place.

- headline: the profile's headline_variant that best fits the posting.
- summary: 2 or 3 sentences that say what the candidate does and the outcome, for this kind of employer, with one standout proof point. Lead with the work, never with tenure ("Spent nearly 7 years..."). No adjective strings, no semicolon run-ons, no list of every tool.
- experience: every role in the profile, most recent first, using its id.
  - Bullets follow action + context + result, under about 25 words, one idea per bullet: never join two ideas with a semicolon. Start each bullet in a role with a different strong verb. Keep numbers exactly as written.
  - Choose the bullets that matter for this posting: 3 to 5 for roles that are relevant to it, 1 for a current role that's unrelated to it.
  - Cut implementation trivia a recruiter doesn't care about, like how credentials were stored or which auth method an API used.
  - A role that ended more than about 8 years ago and is unrelated to the posting gets brief: true and no bullets; it's shown as one line under Earlier experience.
  - A bullet may only cite highlights from its own role, plus answers and summary facts about that role.
- projects: for any technical, AI or engineering role, include every project that's relevant to the posting; they're often the strongest evidence. One sentence each about what it does and what's notable. Don't repeat the project's name; it's shown automatically.
- skills: 3 or 4 short groups of concrete tools and techniques a recruiter would search for, most relevant first. Only skills in the profile or named in an answer. Leave out practices that aren't searchable skills, like "tests", "CI checks", "validation guardrails" or an in-house system.
- Use the posting's own terms where the profile genuinely supports them, because applicant tracking systems match on those words, but never stuff keywords.
- Follow the profile's preferences: its tone, its page limit (about 450 words of resume per page) and its avoid_words.
`)
	writeContext(&b, posting, a, answers)
	return b.String()
}

// coverLetterPrompt asks for the cover letter, given the finished resume so
// the letter can explain rather than repeat it.
func coverLetterPrompt(posting string, a *Analysis, answers []Answer, resume *Draft) string {
	var b strings.Builder
	b.WriteString(`Write the cover letter for this posting, as the candidate, in the first person, to a hiring manager who has read a hundred of these this week. It should read like a capable person wrote a note to one specific company, not like a document generator filled in a template. The resume (below) already lists everything; the letter's job is to make the reader want to talk to this person.

Before writing, decide the one thing you want the reader to believe about this candidate for this role, and the one or two pieces of evidence that prove it best. Rank the candidate's stories and highlights by how well they answer this posting's biggest problems, and pick from the top. Don't default to the same stories every time; another may fit this employer better.

Shape: 250 to 350 words in 3 or 4 paragraphs.
- Open with something specific to this company or role and why it connects to the candidate: a reason from their stories, learned facts or answers. Say something about the company itself if the posting gives you anything beyond the role; if it doesn't, keep it short and don't invent.
- Then tell one or two stories in a few sentences each: the problem, what the candidate figured out or decided, and what changed. Depth beats breadth. Tie each one to a problem this employer named in the posting.
- Close plainly in one or two sentences: a concrete next step, no begging.

Voice:
- Each paragraph makes one point and leads into the next. Connect ideas with because, so, which, and that's why; don't stack facts.
- Vary sentence length and openings. Never start three sentences in a row with "I", and start at most one sentence per paragraph that way where you can.
- Contractions are fine. Plain words. An honest opinion or a lesson learned is welcome when the profile supports it.
- Use at most three numbers in the whole letter, and only inside a story.

Never:
- open with "I am writing to", "I'm excited to apply", the job title, or by restating the posting back to them ("Your posting describes...", "You need someone who...");
- narrate the career in order ("I spent nearly 7 years at...") or list skills or tools in sentences ("I have written Python, TypeScript and SQL");
- lead with an unrelated current job, work authorization or a bare link. Mention these later and briefly, or not at all; the links are in the letterhead;
- copy resume bullets, or use buzzwords: passionate, leverage, synergy, results-driven, dynamic, proven track record, "I believe I would be a great fit", "hit the ground running";
- chain em dashes or pile up three adjectives.

Here is the difference, with an example candidate's facts (not this candidate's):

BAD (a fact dump):
"I spent 4 years at Acme Logistics as a Senior Software Engineer. I built a Claude-based ticket triage service that routes 1,200 support tickets a week. I cut first-response time 40%. I also automated invoice data entry, saving 30 hours a week. I use Python, PostgreSQL and AWS Lambda."

GOOD (the same facts, told):
"Support at Acme had a problem I suspect you'll recognize: tickets came in faster than anyone could decide who should handle them. So I built a triage service on Claude that reads each ticket and routes it to the right team, about 1,200 a week now, and first responses got 40% faster. What I took from it is that the slow part of support is rarely the answer. It's the sorting, and that's the part worth automating first."

The good version adds no facts. Everything it adds is framing: the problem as the reader would see it, why the work mattered, and a lesson the candidate drew from it.

Sources: cite the IDs behind each paragraph's facts about the candidate. A paragraph that makes no factual claim about the candidate, like a short opening about the company or the close, can have empty sources.

Write a full draft, then reread it as that hiring manager. Rewrite anything that sounds templated, generic or like a list, then return the final version.

- greeting: "Dear <company> hiring team," unless the posting names a person.
- closing: a sign-off such as "Sincerely," without the name.
`)
	data, _ := json.MarshalIndent(struct {
		Headline   string        `json:"headline"`
		Summary    string        `json:"summary"`
		Experience []RoleDraft   `json:"experience"`
		Projects   []ProjectLine `json:"projects"`
	}{resume.Headline, resume.Summary.Text, resume.Experience, resume.Projects}, "", "  ")
	b.WriteString("\n<resume>\n")
	b.Write(data)
	b.WriteString("\n</resume>\n")
	writeContext(&b, posting, a, answers)
	return b.String()
}

// writeContext appends the analysis, any answers and the posting.
func writeContext(b *strings.Builder, posting string, a *Analysis, answers []Answer) {
	analysis, _ := json.MarshalIndent(a, "", "  ")
	b.WriteString("\n<analysis>\n")
	b.Write(analysis)
	b.WriteString("\n</analysis>\n")
	if len(answers) > 0 {
		b.WriteString("\n<answers>\n")
		for _, an := range answers {
			fmt.Fprintf(b, "- %s\n  Q: %s\n  A: %s\n", an.QuestionID, an.Question, an.Text)
		}
		b.WriteString("</answers>\n")
	}
	b.WriteString("\n<posting>\n")
	b.WriteString(posting)
	b.WriteString("\n</posting>")
}

// retryPrompt adds the previous attempt and what was wrong with it. Problems
// must be fixed; warnings are about style and should be fixed where possible.
func retryPrompt(prompt string, prev any, problems, warnings []string) string {
	data, _ := json.MarshalIndent(prev, "", "  ")
	var b strings.Builder
	b.WriteString(prompt + "\n\nYour previous attempt is below.\n")
	if len(problems) > 0 {
		b.WriteString("\nIt failed these checks, which must all be fixed:\n- " + strings.Join(problems, "\n- ") + "\n")
		b.WriteString("If a line can't be supported by a real source, remove it rather than citing something that doesn't support it.\n")
	}
	if len(warnings) > 0 {
		b.WriteString("\nThese parts read badly; fix them:\n- " + strings.Join(warnings, "\n- ") + "\n")
	}
	b.WriteString("\n<previous_attempt>\n" + string(data) + "\n</previous_attempt>")
	return b.String()
}
