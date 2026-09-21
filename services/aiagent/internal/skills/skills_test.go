package skills

import (
	"strings"
	"testing"
)

func TestByNameLoadsEmbeddedSkills(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: IssueTriage, want: "电商问题诊断"},
		{name: PurchaseAdvisor, want: "商品选购顾问"},
	}
	for _, tc := range cases {
		document, ok := ByName(tc.name)
		if !ok {
			t.Fatalf("ByName(%q) not found", tc.name)
		}
		if document.Name != tc.name {
			t.Fatalf("name = %q, want %q", document.Name, tc.name)
		}
		if document.Description == "" {
			t.Fatalf("%s description is empty", tc.name)
		}
		if !strings.Contains(document.Content, tc.want) {
			t.Fatalf("%s content missing title %q", tc.name, tc.want)
		}
		if strings.Contains(document.Content, "name: ") {
			t.Fatalf("%s content still contains frontmatter", tc.name)
		}
	}
}

func TestByNameRejectsUnknownSkill(t *testing.T) {
	if _, ok := ByName("not-exist"); ok {
		t.Fatal("unknown skill should not be found")
	}
}

func TestParseRejectsInvalidFrontmatter(t *testing.T) {
	cases := map[string]string{
		"missing delimiter":  "# 只有正文",
		"missing close":      "---\nname: x\n",
		"missing name":       "---\ndescription: d\n---\n正文",
		"missing content":    "---\nname: x\ndescription: d\n---\n",
	}
	for name, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse(%s) expected error", name)
		}
	}
}
