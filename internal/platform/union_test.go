package platform

import "testing"

func TestMergeAddOnly(t *testing.T) {
	base := "Growing it:\n  - Scaling\n  - Roadmap\n  - Open source\n"
	for _, c := range []struct {
		name, ours, theirs, want string
		ok                       bool
	}{
		{name: "both added after the same line: both stay, the default branch's first",
			ours:   "Growing it:\n  - Scaling\n  - Roadmap\n  - Previews\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap\n  - AI tests\n  - Open source\n",
			want:   "Growing it:\n  - Scaling\n  - Roadmap\n  - AI tests\n  - Previews\n  - Open source\n", ok: true},
		{name: "changes in different places",
			ours:   "Growing it:\n  - Scaling (ours)\n  - Roadmap\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap\n  - Open source (theirs)\n",
			want:   "Growing it:\n  - Scaling (ours)\n  - Roadmap\n  - Open source (theirs)\n", ok: true},
		{name: "the same change on both sides",
			ours:   "Growing it:\n  - Scaling\n  - Roadmap!\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap!\n  - Open source\n",
			want:   "Growing it:\n  - Scaling\n  - Roadmap!\n  - Open source\n", ok: true},
		{name: "both added at the end, as two translations",
			ours:   base + "  - ours\n",
			theirs: base + "  - theirs\n",
			want:   base + "  - theirs\n  - ours\n", ok: true},
		{name: "both changed the same line: a person decides",
			ours:   "Growing it:\n  - Scaling\n  - Roadmap (ours)\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap (theirs)\n  - Open source\n"},
		{name: "one changed a line the other added after: they touch",
			ours:   "Growing it:\n  - Scaling\n  - Roadmap (ours)\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap\n  - AI tests\n  - Open source\n"},
		{name: "one deleted what the other changed",
			ours:   "Growing it:\n  - Scaling\n  - Open source\n",
			theirs: "Growing it:\n  - Scaling\n  - Roadmap (theirs)\n  - Open source\n"},
	} {
		got, ok := mergeAddOnly([]byte(base), []byte(c.ours), []byte(c.theirs))
		if ok != c.ok || (ok && string(got) != c.want) {
			t.Errorf("%s: ok=%v\n%s", c.name, ok, got)
		}
	}
	// A file both added from nothing: both lists of lines stay.
	if got, ok := mergeAddOnly(nil, []byte("- a\n"), []byte("- b\n")); !ok || string(got) != "- b\n- a\n" {
		t.Errorf("new on both sides: %v %q", ok, got)
	}
	if _, ok := mergeAddOnly([]byte("x\x00"), []byte("y"), []byte("z")); ok {
		t.Error("a binary file was joined")
	}
}
