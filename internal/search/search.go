package search

import (
	"fmt"
	"regexp"
	"time"

	"github.com/lodibrahim/logpond/internal/parser"
	"github.com/lodibrahim/logpond/internal/store"
)

type Query struct {
	Text   string            // regex match against body + raw (body first, short-circuit)
	Fields map[string]string // exact match against Entry.Fields keys
	Level  string            // severity filter
	After  time.Time         // entries strictly after this time
	Before time.Time         // entries strictly before this time
	Limit  int               // max results, returns newest N (0 = all)
}

func Run(s *store.Store, q Query) ([]*parser.Entry, error) {
	var re *regexp.Regexp
	if q.Text != "" {
		var err error
		re, err = regexp.Compile("(?i)" + q.Text)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}

	all := s.All()
	var results []*parser.Entry

	for _, entry := range all {
		if !matches(entry, q, re) {
			continue
		}
		results = append(results, entry)
	}

	// Limit returns the newest (last) N matches in chronological order
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[len(results)-q.Limit:]
	}

	return results, nil
}

func matches(entry *parser.Entry, q Query, re *regexp.Regexp) bool {
	if q.Level != "" && entry.Severity != q.Level {
		return false
	}

	if !q.After.IsZero() && !entry.Timestamp.After(q.After) {
		return false
	}
	if !q.Before.IsZero() && !entry.Timestamp.Before(q.Before) {
		return false
	}

	for k, v := range q.Fields {
		if entry.Fields[k] != v {
			return false
		}
	}

	if re != nil {
		if re.MatchString(entry.Body) || re.MatchString(entry.Raw) {
			return true
		}
		for _, v := range entry.Fields {
			if re.MatchString(v) {
				return true
			}
		}
		return false
	}

	return true
}
