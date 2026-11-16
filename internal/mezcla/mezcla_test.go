package mezcla

import "testing"

func TestMerge(t *testing.T) {
	base := "a\nb\nc\nd\ne\n"
	cases := []struct {
		name, ours, theirs, want string
		conflict                 bool
	}{
		{"different places", "A\nb\nc\nd\ne\n", "a\nb\nc\nd\nE\n", "A\nb\nc\nd\nE\n", false},
		{"the same change", "a\nB\nc\nd\ne\n", "a\nB\nc\nd\ne\n", "a\nB\nc\nd\ne\n", false},
		{"both only added at one place", "a\nb\nours\nc\nd\ne\n", "a\nb\ntheirs\nc\nd\ne\n", "a\nb\ntheirs\nours\nc\nd\ne\n", false},
		{"both changed one line", "a\nb\nC1\nd\ne\n", "a\nb\nC2\nd\ne\n",
			"a\nb\n<<<<<<< rama\nC1\n=======\nC2\n>>>>>>> main\nd\ne\n", true},
		{"touching changes grow one conflict", "a\nB\nC\nd\ne\n", "a\nb\nX\nD\ne\n",
			"a\n<<<<<<< rama\nB\nC\nd\n=======\nb\nX\nD\n>>>>>>> main\ne\n", true},
		{"no newline at the end", "a\nb\nc\nd\nE1", "a\nb\nc\nd\nE2", "a\nb\nc\nd\n<<<<<<< rama\nE1\n=======\nE2\n>>>>>>> main\n", true},
	}
	for _, c := range cases {
		got, conflict, ok := Merge([]byte(base), []byte(c.ours), []byte(c.theirs), "rama", "main")
		if !ok || conflict != c.conflict || string(got) != c.want {
			t.Errorf("%s: conflict=%v ok=%v\n%s\nwant\n%s", c.name, conflict, ok, got, c.want)
		}
		if HasMarks(got) != c.conflict {
			t.Errorf("%s: HasMarks=%v", c.name, HasMarks(got))
		}
	}
	if _, _, ok := Merge([]byte("a\x00"), []byte("b"), []byte("c"), "rama", "main"); ok {
		t.Error("a binary file was merged")
	}
}
