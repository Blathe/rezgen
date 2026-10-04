// Package profile defines the candidate profile: the single source of truth
// that every tailored resume and cover letter is drawn from.
package profile

// SchemaVersion is the profile format version this build understands.
const SchemaVersion = 1

// Profile is the full set of facts about the candidate. A tailored resume
// selects and rewords from it; nothing on a generated page may come from
// anywhere else.
type Profile struct {
	SchemaVersion      int                 `json:"schema_version"`
	Contact            Contact             `json:"contact"`
	HeadlineVariants   []string            `json:"headline_variants,omitempty"`
	SummaryFacts       []string            `json:"summary_facts,omitempty"`
	Experience         []Experience        `json:"experience"`
	Projects           []Project           `json:"projects,omitempty"`
	Skills             map[string][]string `json:"skills,omitempty"`
	Education          []Education         `json:"education,omitempty"`
	Certifications     []Certification     `json:"certifications,omitempty"`
	CoverLetterStories []Story             `json:"cover_letter_stories,omitempty"`
	Preferences        Preferences         `json:"preferences,omitempty"`
	LearnedFacts       []LearnedFact       `json:"learned_facts,omitempty"`
}

// LearnedFact is the candidate's answer to a question rezgen asked while
// tailoring an application. Saving it means later runs don't ask again and
// can cite it like any other profile entry.
type LearnedFact struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Learned is the date it was saved, as YYYY-MM-DD.
	Learned string `json:"learned,omitempty"`
	// Context names the posting that prompted the question.
	Context string `json:"context,omitempty"`
}

type Contact struct {
	Name     string            `json:"name"`
	Email    string            `json:"email"`
	Phone    string            `json:"phone,omitempty"`
	Location string            `json:"location,omitempty"`
	Links    map[string]string `json:"links,omitempty"`
}

type Experience struct {
	ID         string      `json:"id"`
	Company    string      `json:"company"`
	Title      string      `json:"title"`
	Location   string      `json:"location,omitempty"`
	Start      string      `json:"start"`
	End        *string     `json:"end"`
	Context    string      `json:"context,omitempty"`
	Highlights []Highlight `json:"highlights"`
}

// Highlight is one factual accomplishment. Stable IDs let generated text be
// traced back to the profile entry it came from.
type Highlight struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Metrics []string `json:"metrics,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

type Project struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url,omitempty"`
	Text   string   `json:"text"`
	Skills []string `json:"skills,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type Education struct {
	School string `json:"school"`
	Degree string `json:"degree,omitempty"`
	Field  string `json:"field,omitempty"`
	End    string `json:"end,omitempty"`
}

type Certification struct {
	Name   string `json:"name"`
	Issuer string `json:"issuer,omitempty"`
	Date   string `json:"date,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Story is a short paragraph about motivation or a notable win that cover
// letters can draw on.
type Story struct {
	ID   string   `json:"id"`
	Text string   `json:"text"`
	Tags []string `json:"tags,omitempty"`
}

type Preferences struct {
	Tone       string   `json:"tone,omitempty"`
	MaxPages   int      `json:"max_pages,omitempty"`
	AvoidWords []string `json:"avoid_words,omitempty"`
	// ProjectsPosition puts the Projects section "before" Experience (the
	// default, since projects are often the strongest evidence for technical
	// roles) or "after" it.
	ProjectsPosition string `json:"projects_position,omitempty"`
}

// Current reports whether the role has no end date.
func (e Experience) Current() bool { return e.End == nil }
