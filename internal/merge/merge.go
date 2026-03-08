// Package merge provides utilities for merging quiz question stages.
package merge

import (
	"github.com/jh125486/pdf2qti/internal/render"
)

// Merge combines TF, MA, and MC question slices, renumbering sequentially.
func Merge(tf, ma, mc []render.Question) []render.Question {
	all := make([]render.Question, 0, len(tf)+len(ma)+len(mc))
	all = append(all, tf...)
	all = append(all, ma...)
	all = append(all, mc...)
	for i := range all {
		all[i].Number = i + 1
	}
	return all
}

// SplitDraft extracts TF, MA, MC questions from a merged slice.
// The returned slices are re-numbered per-section starting at 1.
func SplitDraft(questions []render.Question, tfCount, maCount, mcCount int) (tf, ma, mc []render.Question) {
	n := 0
	for i := 0; i < tfCount && n < len(questions); i++ {
		q := questions[n]
		q.Number = i + 1
		tf = append(tf, q)
		n++
	}
	for i := 0; i < maCount && n < len(questions); i++ {
		q := questions[n]
		q.Number = i + 1
		ma = append(ma, q)
		n++
	}
	for i := 0; i < mcCount && n < len(questions); i++ {
		q := questions[n]
		q.Number = i + 1
		mc = append(mc, q)
		n++
	}
	return tf, ma, mc
}
