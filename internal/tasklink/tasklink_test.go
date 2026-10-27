package tasklink

import (
	"testing"
)

func find(refs []Reference, id string) *Reference {
	for i := range refs {
		if refs[i].ID == id {
			return &refs[i]
		}
	}
	return nil
}

func TestClickUpConventions(t *testing.T) {
	refs := Extract([]Text{
		{Source: SourceBranch, Body: "CU-86b1x2y3z_Login-con-SSO_juan"},
		{Source: SourceTitle, Body: "Login SSO (cu-86B1X2Y3Z)"},
		{Source: SourceCommit, Ref: "abc", Body: "fix: token refresh CU-9ab3cd[in review]\n\nsee https://app.clickup.com/t/86c0aa11b"},
		{Source: SourceDescription, Body: "Spec: https://app.clickup.com/t/9012345/ENG-42"},
	}, nil)

	r := find(refs, "86b1x2y3z")
	if r == nil || r.Confidence != "high" || r.Kind != KindTaskID || len(r.FoundIn) != 2 {
		t.Fatalf("branch/title ref: %+v", r)
	}
	if r.URL != "https://app.clickup.com/t/86b1x2y3z" {
		t.Errorf("url = %q", r.URL)
	}
	// ClickUp's [status] syntax has no spaces; "[in review]" is not a hint.
	if r := find(refs, "9ab3cd"); r == nil || r.StatusHint != "" {
		t.Errorf("status with spaces must not be captured: %+v", r)
	}
	if r := find(refs, "86c0aa11b"); r == nil || r.FoundIn[0].Ref != "abc" {
		t.Errorf("url ref in commit: %+v", r)
	}
	if r := find(refs, "ENG-42"); r == nil || r.Kind != KindCustomID {
		t.Errorf("custom id from url without configured prefix: %+v", r)
	}
	if refs[0].ID != "86b1x2y3z" {
		t.Errorf("order must follow first appearance, got %s first", refs[0].ID)
	}
}

func TestStatusHint(t *testing.T) {
	refs := Extract([]Text{{Source: SourceCommit, Body: "done CU-ab12cd[ready] and ENG-7[qa]"}}, []string{"eng"})
	if r := find(refs, "ab12cd"); r == nil || r.StatusHint != "ready" {
		t.Errorf("CU status: %+v", r)
	}
	if r := find(refs, "ENG-7"); r == nil || r.StatusHint != "qa" {
		t.Errorf("custom status: %+v", r)
	}
}

func TestCustomPrefixesOnlyWhenConfigured(t *testing.T) {
	text := []Text{{Source: SourceBranch, Body: "feature/ENG-123-add-export"}}
	if refs := Extract(text, nil); len(refs) != 0 {
		t.Errorf("without prefixes, PREFIX-n must not match: %+v", refs)
	}
	refs := Extract(text, []string{" eng "})
	if len(refs) != 1 || refs[0].ID != "ENG-123" || refs[0].Confidence != "high" {
		t.Errorf("got %+v", refs)
	}
}

func TestHashIDsAreLowConfidenceAndFiltered(t *testing.T) {
	refs := Extract([]Text{{Source: SourceDescription, Body: "Closes #86b1x2y3z. Not: #123456, #readme, &#x27abc1;, docs.html#sec2a3b4, PR #42"}}, nil)
	if len(refs) != 1 {
		t.Fatalf("got %+v", refs)
	}
	if refs[0].ID != "86b1x2y3z" || refs[0].Confidence != "low" || refs[0].FoundIn[0].Match != "#86b1x2y3z" {
		t.Errorf("got %+v", refs[0])
	}
}

func TestConfidenceUpgradesWhenSeenWithStrongForm(t *testing.T) {
	refs := Extract([]Text{
		{Source: SourceDescription, Body: "related #86b1x2y3z"},
		{Source: SourceBranch, Body: "CU-86b1x2y3z_x"},
	}, nil)
	if len(refs) != 1 || refs[0].Confidence != "high" || len(refs[0].FoundIn) != 2 {
		t.Errorf("got %+v", refs)
	}
}
