// Package audit is the append-only, hash-chained record of every decision.
// Each entry carries the hash of the previous one, so a tampered file fails
// verification. Storage is a JSONL file today; the chain is what matters.
package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	Seq      int64           `json:"seq"`
	At       time.Time       `json:"at"`
	Actor    string          `json:"actor"`
	Action   string          `json:"action"` // change.proposed | change.approved | change.rejected | change.applied | change.failed
	ChangeID string          `json:"changeId"`
	Cluster  string          `json:"cluster"`
	Detail   json.RawMessage `json:"detail,omitempty"`
	Prev     string          `json:"prev"`
	Hash     string          `json:"hash"`
}

type Log struct {
	mu   sync.Mutex
	path string
	last Entry
	n    int64
}

// Open loads the chain tail from path, creating the file if needed.
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := &Log{path: path}
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			l.last = e
			l.n = e.Seq
		}
	}
	return l, nil
}

func hashOf(e Entry) (string, error) {
	e.Hash = ""
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Append records an entry and returns it with seq, prev and hash filled.
//
// Every failure leaves the chain untouched. An entry that cannot be encoded
// is refused rather than written as a blank line, because a log that breaks
// its own chain on bad input is a log an attacker can break on purpose.
func (l *Log) Append(e Entry) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Seq = l.n + 1
	e.At = time.Now().UTC()
	e.Prev = l.last.Hash
	h, err := hashOf(e)
	if err != nil {
		return e, fmt.Errorf("audit entry cannot be encoded: %w", err)
	}
	e.Hash = h
	b, err := json.Marshal(e)
	if err != nil {
		return e, fmt.Errorf("audit entry cannot be encoded: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return e, err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return e, err
	}
	l.n = e.Seq
	l.last = e
	return e, l.writeHead(e)
}

// head is the anchor that makes a truncated tail detectable. The chain alone
// cannot see its own end: lopping off the last entries leaves every remaining
// link intact. Recording where the chain ended, outside the chain, is what
// turns "the links are sound" into "nothing was removed".
type head struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

func (l *Log) headPath() string { return l.path + ".head" }

func (l *Log) writeHead(e Entry) error {
	b, err := json.Marshal(head{Seq: e.Seq, Hash: e.Hash})
	if err != nil {
		return err
	}
	tmp := l.headPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.headPath())
}

func (l *Log) readHead() (*head, error) {
	b, err := os.ReadFile(l.headPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h head
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, fmt.Errorf("audit head is unreadable: %w", err)
	}
	return &h, nil
}

// Entries returns the newest `limit` entries.
func (l *Log) Entries(limit int) ([]Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

// Verify walks the chain and reports the first broken link, if any.
//
// It reads the file line by line rather than through Entries, because a line
// Entries would skip as unparseable is exactly what a Verify must catch.
func (l *Log) Verify() (ok bool, brokenSeq int64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	anchor, err := l.readHead()
	if err != nil {
		return false, 0, err
	}

	f, err := os.Open(l.path)
	if err != nil {
		return false, 0, err
	}
	defer f.Close()

	prev, hashAtAnchor := "", ""
	var count int64
	var last Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		count++
		if len(line) == 0 {
			return false, count, nil
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return false, count, nil
		}
		if e.Seq != count {
			return false, e.Seq, nil
		}
		h, err := hashOf(e)
		if err != nil {
			return false, e.Seq, err
		}
		if e.Prev != prev || h != e.Hash {
			return false, e.Seq, nil
		}
		prev, last = e.Hash, e
		if anchor != nil && e.Seq == anchor.Seq {
			hashAtAnchor = e.Hash
		}
	}
	if err := sc.Err(); err != nil {
		return false, 0, err
	}

	if anchor != nil {
		switch {
		case last.Seq < anchor.Seq:
			// The chain ends before it used to: entries were removed.
			return false, last.Seq + 1, nil
		case hashAtAnchor != anchor.Hash:
			return false, anchor.Seq, nil
		}
	}
	return true, 0, nil
}
