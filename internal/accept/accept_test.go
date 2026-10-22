package accept

import (
	"context"
	"testing"
)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name     string
		spec     Spec
		accepted bool
		code     string
	}{
		{"skeleton ok", Spec{Kind: KindSkeletonBuild, Build: "exit 0"}, true, ""},
		{"skeleton build fail", Spec{Kind: KindSkeletonBuild, Build: "exit 2"}, false, CodeBuildFailed},
		{"green ok", Spec{Kind: KindGreenTests, Build: "exit 0", Run: "exit 0"}, true, ""},
		{"green fail", Spec{Kind: KindGreenTests, Build: "exit 0", Run: "exit 1"}, false, CodeAcceptanceFailed},
		{"red ok", Spec{Kind: KindRedTests, Build: "exit 0", Run: "exit 1"}, true, ""},
		{"red not red", Spec{Kind: KindRedTests, Build: "exit 0", Run: "exit 0"}, false, CodeTestsNotRed},
		{"red build fail", Spec{Kind: KindRedTests, Build: "exit 9", Run: "exit 1"}, false, CodeBuildFailed},
		{"unknown kind", Spec{Kind: "nope", Build: "exit 0"}, false, CodeUnknownKind},
		{"no commands", Spec{Kind: KindGreenTests}, false, CodeUnknownKind},
	}
	dir := t.TempDir()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := EvaluateSpec(context.Background(), dir, c.spec)
			if out.Accepted != c.accepted {
				t.Errorf("accepted = %v, want %v (code %s)", out.Accepted, c.accepted, out.Code)
			}
			if !c.accepted && out.Code != c.code {
				t.Errorf("code = %s, want %s", out.Code, c.code)
			}
		})
	}
}

func TestEvaluateInvalidJSON(t *testing.T) {
	out := Evaluate(context.Background(), t.TempDir(), []byte(`{`))
	if out.Code != CodeUnknownKind {
		t.Fatalf("code = %s", out.Code)
	}
}
