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
	b.WriteString(`You help one candidate apply for jobs. You read a job posting, compare it with the candidate's profile, and write a resume and cover letter tailored to that posting.

The profile below is the complete set of facts about the candidate. Its learned_facts are the candidate's answers to questions asked while tailoring earlier applications; treat them as facts like any other entry. The only other facts you may use are answers to this application's questions, which are given with the request that uses them. Never add an employer, title, date, skill, tool, metric, credential or result that is not in the profile or an answer. You may select, reorder, combine, shorten and reword. You may not inflate: keep numbers exactly as written, don't upgrade "helped build" to "led", and don't turn a skill mentioned once into expertise.

Every piece of text you write cites its sources by ID. These are the citable IDs:
- experience ids and highlight ids
- project ids
- cover_letter_stories ids
- learned_facts ids
- summary facts, as fact-1, fact-2, ... in the order listed below
- answers, by the id of the question they answer

Write plainly and specifically. Lead bullets with what the candidate did and the result. Use the posting's own terms where the profile genuinely supports them, because applicant tracking systems match on those words.
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

Then write at most 5 questions for the candidate. Ask only where a true answer could add a real, specific fact that addresses a gap or strengthens a weak match, such as experience the profile doesn't mention or a metric for an existing highlight. Don't ask about anything the profile already answers, and never repeat or rephrase a question in learned_facts: the candidate has answered those, and their answers count as evidence. Don't ask about motivation. If the profile covers the posting well, ask nothing.

<posting>
` + posting + `
</posting>`
}

func writePrompt(posting string, a *Analysis, answers []Answer) string {
	analysis, _ := json.MarshalIndent(a, "", "  ")
	var b strings.Builder
	b.WriteString(`Write a resume and cover letter for this posting.

Resume:
- headline: one of the profile's headline_variants, whichever fits the posting best.
- summary: 2 or 3 sentences drawn from summary facts and the strongest matching highlights.
- experience: include every role in the profile, most recent first, using its id. Give each role the bullets that best fit this posting: 2 to 5 for recent or relevant roles, 1 or 2 for older or less relevant ones. A bullet may only cite highlights from its own role (plus answers and summary facts that are about that role).
- projects: include only projects relevant to the posting; text is one sentence.
- skills: group the profile's skills under short headings, relevant ones first. Use only skills that appear in the profile or that an answer says the candidate has used.
- Follow the profile's preferences: its tone, its page limit (about 450 words of resume per page), and never use its avoid_words.

Cover letter:
- greeting: "Dear <company> hiring team," unless the posting names a person.
- 3 or 4 short paragraphs: why this role, the two or three most relevant pieces of evidence, and a brief close. Draw on cover_letter_stories where they fit.
- closing: a sign-off such as "Sincerely," without the name.
- Don't repeat resume bullets word for word.

<analysis>
`)
	b.Write(analysis)
	b.WriteString("\n</analysis>\n")
	if len(answers) > 0 {
		b.WriteString("\n<answers>\n")
		for _, an := range answers {
			fmt.Fprintf(&b, "- %s\n  Q: %s\n  A: %s\n", an.QuestionID, an.Question, an.Text)
		}
		b.WriteString("</answers>\n")
	}
	b.WriteString("\n<posting>\n")
	b.WriteString(posting)
	b.WriteString("\n</posting>")
	return b.String()
}

func retryPrompt(prompt string, prev *Draft, problems []string) string {
	data, _ := json.MarshalIndent(prev, "", "  ")
	return prompt + `

Your previous draft is below. It failed these checks:
- ` + strings.Join(problems, "\n- ") + `

Fix every problem. If a line can't be supported by a real source, remove it rather than citing something that doesn't support it.

<previous_draft>
` + string(data) + `
</previous_draft>`
}
