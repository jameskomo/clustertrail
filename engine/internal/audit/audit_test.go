package audit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return l, path
}

func TestChainLinksAndVerifies(t *testing.T) {
	l, _ := open(t)
	for _, action := range []string{"change.proposed", "change.approved", "change.applied"} {
		if _, err := l.Append(Entry{Actor: "tester", Action: action, Cluster: "c1"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	entries, err := l.Entries(0)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	// Entries come back newest first; each one must name its predecessor.
	if entries[0].Prev != entries[1].Hash || entries[1].Prev != entries[2].Hash {
		t.Fatal("entries are not linked to their predecessor")
	}
	if entries[2].Prev != "" {
		t.Fatal("the first entry should have no predecessor")
	}
	if ok, seq, err := l.Verify(); err != nil || !ok {
		t.Fatalf("a freshly written chain must verify: ok=%v seq=%d err=%v", ok, seq, err)
	}
}

func TestVerifyDetectsATamperedEntry(t *testing.T) {
	l, path := open(t)
	for i := 0; i < 4; i++ {
		if _, err := l.Append(Entry{Actor: "tester", Action: "change.applied", Cluster: "c1"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the actor on the second entry, exactly the edit the chain exists
	// to catch: someone changing who did something after the fact.
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	lines[1] = strings.Replace(lines[1], `"actor":"tester"`, `"actor":"someone else"`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ok, seq, err := reopened.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatal("a tampered entry must not verify")
	}
	if seq != 2 {
		t.Fatalf("want the break reported at seq 2, got %d", seq)
	}
}

func TestVerifyDetectsARemovedEntry(t *testing.T) {
	l, path := open(t)
	for i := 0; i < 4; i++ {
		if _, err := l.Append(Entry{Actor: "tester", Action: "change.applied"}); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	// Drop the third entry, so the fourth names a predecessor that is gone.
	kept := append(append([]string{}, lines[:2]...), lines[3:]...)
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, _ := Open(path)
	if ok, _, _ := reopened.Verify(); ok {
		t.Fatal("a chain with a removed entry must not verify")
	}
}

func TestAppendSurvivesReopening(t *testing.T) {
	l, path := open(t)
	if _, err := l.Append(Entry{Actor: "a", Action: "change.applied"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := reopened.Append(Entry{Actor: "b", Action: "change.applied"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 2 {
		t.Fatalf("a reopened log must continue the sequence, got seq %d", e.Seq)
	}
	if e.Prev == "" {
		t.Fatal("a reopened log must link to the last entry it read")
	}
	if ok, _, _ := reopened.Verify(); !ok {
		t.Fatal("the chain must still verify across a reopen")
	}
}

// The chain cannot see its own end. Removing the last entries leaves every
// remaining link intact, and those last entries are exactly the approval and
// the apply of the most recent change. An anchor written outside the chain is
// what makes the removal visible.

func TestVerifyDetectsATruncatedTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := l.Append(Entry{Actor: "operator", Action: "change.applied"}); err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimRight(body, "\n"), []byte("\n"))
	kept := bytes.Join(lines[:3], []byte("\n"))
	if err := os.WriteFile(path, append(kept, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ok, broken, err := reopened.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ok {
		t.Fatal("three entries were removed from the end and verification passed")
	}
	if broken != 4 {
		t.Errorf("the break should be reported at the first missing entry, want 4, got %d", broken)
	}
}

func TestVerifyDetectsAnEmptiedLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Entry{Actor: "operator", Action: "change.applied"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := reopened.Verify(); ok {
		t.Error("an emptied log must not verify")
	}
}

// An entry that cannot be encoded used to be written as a blank line, which
// broke the chain permanently and told nobody.

func TestAppendRefusesAnEntryItCannotEncode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Entry{Actor: "operator", Action: "exec.opened"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Entry{Actor: "operator", Action: "exec.opened", Detail: []byte(`{"pod":"x`)}); err == nil {
		t.Error("a malformed detail must be refused, not written as a blank line")
	}
	if _, err := l.Append(Entry{Actor: "operator", Action: "exec.opened"}); err != nil {
		t.Fatalf("the log must still be usable after a refused entry: %v", err)
	}
	ok, broken, err := l.Verify()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Errorf("a refused entry must leave the chain intact, broken at %d", broken)
	}
}
