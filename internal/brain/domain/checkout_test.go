package domain_test

import (
	"testing"

	"github.com/medilies/nerve/internal/brain/domain"
)

func TestLinkRelPlacesSkillsAndAgents(t *testing.T) {
	skill := domain.Link{Kind: "skill", Name: "go-stack"}
	agent := domain.Link{Kind: "agent", Name: "reviewer"}
	for got, want := range map[string]string{
		skill.Rel():  ".claude/skills/go-stack",
		agent.Rel():  ".claude/agents/reviewer.md",
		skill.Item(): "skill go-stack",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestCheckoutAtMatchesACleanedPath(t *testing.T) {
	m := domain.Machine{Checkouts: []domain.Checkout{{Path: "/w/app", Project: "app"}}}
	if c, ok := m.CheckoutAt("/w/app/"); !ok || c.Project != "app" {
		t.Errorf("got %v, %v", c, ok)
	}
	if _, ok := m.CheckoutAt("/w/other"); ok {
		t.Errorf("found a checkout that is not listed")
	}
}
