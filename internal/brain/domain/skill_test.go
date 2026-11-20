package domain_test

import (
	"strings"
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

func TestParseFrontmatterReadsTopLevelKeys(t *testing.T) {
	meta, err := domain.ParseFrontmatter("SKILL.md", "---\r\nname: go-stack\r\ndescription: >\r\n  Go,\r\n  done right.\r\nallowed-tools:\r\n- Bash\r\n- Read\r\nlicense: 'MIT'\r\n# a comment\r\n---\r\n# Body\r\n")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"name": "go-stack", "description": "Go, done right.", "allowed-tools": "- Bash - Read", "license": "MIT"} {
		if meta[k] != want {
			t.Errorf("%s is %q, want %q", k, meta[k], want)
		}
	}
}

func TestParseFrontmatterLeavesCommentsOut(t *testing.T) {
	meta, err := domain.ParseFrontmatter("SKILL.md", "---\nname: foo # the folder's name\ndescription: \"Does \\\"#\\\" things.\" # quoted\nlicense: # none yet\ncompatibility: 'it''s # fine'\n---\n")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"name": "foo", "description": `Does \"#\" things.`, "license": "", "compatibility": "it''s # fine"} {
		if meta[k] != want {
			t.Errorf("%s is %q, want %q", k, meta[k], want)
		}
	}
}

func TestParseFrontmatterReadsAnEmptyOne(t *testing.T) {
	if meta, err := domain.ParseFrontmatter("SKILL.md", "---\n---\n# Body\n"); err != nil || len(meta) != 0 {
		t.Errorf("got %v, %v", meta, err)
	}
}

func TestParseFrontmatterReadsPastAByteOrderMark(t *testing.T) {
	if meta, err := domain.ParseFrontmatter("SKILL.md", "\ufeff---\nname: go-stack\n---\n"); err != nil || meta["name"] != "go-stack" {
		t.Errorf("got %v, %v", meta, err)
	}
}

func TestParseFrontmatterReadsAQuotedKey(t *testing.T) {
	if meta, err := domain.ParseFrontmatter("SKILL.md", "---\n\"name\": go-stack\n'description': Go.\n---\n"); err != nil || meta["name"] != "go-stack" || meta["description"] != "Go." {
		t.Errorf("got %q, %v", meta, err)
	}
}

func TestParseFrontmatterRefusesWhatIsNotFrontmatter(t *testing.T) {
	for text, want := range map[string]string{
		"# no frontmatter\n":     "SKILL.md has no frontmatter",
		"---\nname: x\n":         "SKILL.md frontmatter is not closed",
		"---\nnot a pair\n---\n": `frontmatter line "not a pair" is not key: value`,
	} {
		if _, err := domain.ParseFrontmatter("SKILL.md", text); err == nil || err.Error() != want {
			t.Errorf("got %v, want %q", err, want)
		}
	}
}

func TestSkillProblems(t *testing.T) {
	ok := domain.Skill{Name: "go-stack", Meta: map[string]string{"name": "go-stack", "description": strings.Repeat("é", 1024)}}
	if p := ok.Problems(); len(p) > 0 {
		t.Errorf("a valid skill, its description counted in characters, has problems: %v", p)
	}
	bad := domain.Skill{Name: "Go_Stack", Meta: map[string]string{"name": "other"}}
	got := strings.Join(bad.Problems(), "\n")
	for _, want := range []string{`frontmatter name "other" differs from folder name`, "lowercase words joined by hyphens", "no description"} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
	long := domain.Skill{Name: "x", Meta: map[string]string{"name": "x", "description": strings.Repeat("a", 1025)}}
	if got := long.Problems(); len(got) != 1 || got[0] != "description is 1025 characters, over 1024" {
		t.Errorf("problems are %v", got)
	}
}

func TestClaudeAIRejectionNamesTheKeysItRefuses(t *testing.T) {
	s := domain.Skill{Meta: map[string]string{"name": "x", "description": "y", "model": "opus", "context": "fork"}}
	if got := s.ClaudeAIRejection(); got != "frontmatter keys claude.ai rejects: context, model" {
		t.Errorf("got %q", got)
	}
	s.Meta = map[string]string{"name": "x", "description": "y", "allowed-tools": "Bash"}
	if got := s.ClaudeAIRejection(); got != "" {
		t.Errorf("an accepted skill is refused: %q", got)
	}
}

func TestAgentProblem(t *testing.T) {
	if err := domain.AgentProblem("reviewer", map[string]string{"name": "reviewer", "description": "Reviews."}); err != nil {
		t.Errorf("a valid agent: %v", err)
	}
	if err := domain.AgentProblem("reviewer", map[string]string{"name": "other"}); err == nil || !strings.Contains(err.Error(), "differs from file name") {
		t.Errorf("got %v", err)
	}
	if err := domain.AgentProblem("reviewer", map[string]string{"name": "reviewer"}); err == nil || err.Error() != "no description" {
		t.Errorf("got %v", err)
	}
}
