package cli

import "testing"

func TestBlueprintNameToDisplayStringMatchesUECases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		raw    string
		isBool bool
		want   string
	}{
		{name: "bool prefix", raw: "bTest", isBool: true, want: "Test"},
		{name: "bool camel", raw: "bTwoWords", isBool: true, want: "Two Words"},
		{name: "bool lowercase", raw: "boolean", isBool: true, want: "Boolean"},
		{name: "non bool prefix", raw: "bNotBoolean", isBool: false, want: "B Not Boolean"},
		{name: "bool no prefix", raw: "NonprefixBoolean", isBool: true, want: "Nonprefix Boolean"},
		{name: "lower bool no prefix", raw: "lowerNonprefixBoolean", isBool: true, want: "Lower Nonprefix Boolean"},
		{name: "camel", raw: "lowerCase", want: "Lower Case"},
		{name: "with underscores", raw: "With_Underscores", want: "With Underscores"},
		{name: "lower underscores", raw: "lower_underscores", want: "Lower Underscores"},
		{name: "mixed underscores", raw: "mixed_Underscores", want: "Mixed Underscores"},
		{name: "underscores", raw: "Mixed_underscores", want: "Mixed Underscores"},
		{name: "article", raw: "ArticleInString", want: "Article in String"},
		{name: "or article", raw: "OneOrTwo", want: "One or Two"},
		{name: "and article", raw: "OneAndTwo", want: "One and Two"},
		{name: "article at end", raw: "OneAs", want: "One As"},
		{name: "numeric expression", raw: "-1.5", want: "-1.5"},
		{name: "integer numeric", raw: "1234", want: "1234"},
		{name: "decimal numeric", raw: "1234.5", want: "1234.5"},
		{name: "negative decimal numeric", raw: "-1234.5", want: "-1234.5"},
		{name: "parens", raw: "Text (in parens)", want: "Text (In Parens)"},
		{name: "3d", raw: "Text3D", want: "Text 3D"},
		{name: "caps", raw: "PluralCAPs", want: "Plural CAPs"},
		{name: "fbx", raw: "FBXEditor", want: "FBXEditor"},
		{name: "fbx underscore", raw: "FBX_Editor", want: "FBX Editor"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := blueprintNameToDisplayString(tc.raw, tc.isBool); got != tc.want {
				t.Fatalf("blueprintNameToDisplayString(%q, %t): got %q want %q", tc.raw, tc.isBool, got, tc.want)
			}
		})
	}
}
