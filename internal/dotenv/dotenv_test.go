package dotenv

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	src := "\ufeff# comment\n" +
		"PLAIN=value\n" +
		"export EXPORTED=1\n" +
		"EMPTY=\n" +
		"SPACED = spaced value # trailing comment\n" +
		"ONLY_COMMENT= # nothing\n" +
		"DQ=\"a \\\" quote\"\n" +
		"EMPTY_DQ=\"\"\n" +
		"SQ='single'\n" +
		"MULTI=\"line one\nline two\nNOT_A_KEY=inside\"\n" +
		"AFTER=1\r\n" +
		"not an assignment\n" +
		"1BAD=x\n" +
		"dotted.key=ok\n"
	got := Parse([]byte(src))
	want := []Key{
		{"PLAIN", false, 2},
		{"EXPORTED", false, 3},
		{"EMPTY", true, 4},
		{"SPACED", false, 5},
		{"ONLY_COMMENT", true, 6},
		{"DQ", false, 7},
		{"EMPTY_DQ", true, 8},
		{"SQ", false, 9},
		{"MULTI", false, 10},
		{"AFTER", false, 13},
		{"dotted.key", false, 16},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestNamesSortedUnique(t *testing.T) {
	got := Names([]byte("B=1\nA=2\nB=3\n"))
	if !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDiff(t *testing.T) {
	added, removed := Diff([]string{"A", "B", "C"}, []string{"B", "C", "D"})
	if !reflect.DeepEqual(added, []string{"D"}) || !reflect.DeepEqual(removed, []string{"A"}) {
		t.Fatalf("added=%v removed=%v", added, removed)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("A=1\nB=\"x\ny\"\n"))
	f.Add([]byte("export X='unterminated"))
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, k := range Parse(data) {
			if !keyRe.MatchString(k.Name) {
				t.Fatalf("invalid key %q", k.Name)
			}
		}
	})
}
