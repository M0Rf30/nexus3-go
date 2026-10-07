package version

import (
	"regexp"
	"slices"
	"strings"
)

// mavenTimestamp matches a timestamped snapshot: <base>-yyyyMMdd.HHmmss-N.
var mavenTimestamp = regexp.MustCompile(`^(.+)-(\d{8})\.(\d{6})-(\d+)$`)

func compareMaven(a, b string) int {
	ka, kb := newMavenKey(a), newMavenKey(b)

	if c := ka.root.cmp(kb.root); c != 0 {
		return c
	}

	if c := strings.Compare(ka.stamp, kb.stamp); c != 0 {
		return c
	}

	return compareDigitStrings(ka.build, kb.build)
}

// mavenKey is the sort key of a Maven version: a ComparableVersion tree
// plus, for timestamped snapshots, the timestamp and build number.
type mavenKey struct {
	root         *mavenList
	stamp, build string
}

func newMavenKey(v string) mavenKey {
	v = strings.TrimSpace(v)

	if m := mavenTimestamp.FindStringSubmatch(v); m != nil {
		return mavenKey{root: parseMaven(m[1] + "-SNAPSHOT"), stamp: m[2] + m[3], build: m[4]}
	}

	return mavenKey{root: parseMaven(v), build: "0"}
}

// mavenItem is one element of a ComparableVersion tree.
type mavenItem interface {
	cmp(o mavenItem) int
	isNull() bool
}

type mavenInt struct{ v string }

type mavenStr struct{ v string }

type mavenList struct{ items []mavenItem }

const (
	qAlpha     = "alpha"
	qBeta      = "beta"
	qMilestone = "milestone"
	qSnapshot  = "snapshot"
)

var qualifiers = []string{qAlpha, qBeta, qMilestone, "rc", qSnapshot, "", "sp"}

const releaseIndex = "5"

func newMavenInt(s string) mavenInt {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		s = "0"
	}

	return mavenInt{v: s}
}

func newMavenStr(s string, followedByDigit bool) mavenStr {
	if followedByDigit && len(s) == 1 {
		switch s[0] {
		case 'a':
			s = qAlpha
		case 'b':
			s = qBeta
		case 'm':
			s = qMilestone
		}
	}

	switch s {
	case "ga", "final", "release":
		s = ""
	case "cr":
		s = "rc"
	}

	return mavenStr{v: s}
}

func (s mavenStr) comparable() string {
	for i, q := range qualifiers {
		if q == s.v {
			return string(rune('0' + i))
		}
	}

	return "7-" + s.v
}

func (i mavenInt) isNull() bool   { return i.v == "0" }
func (s mavenStr) isNull() bool   { return s.comparable() == releaseIndex }
func (l *mavenList) isNull() bool { return len(l.items) == 0 }

func (i mavenInt) cmp(o mavenItem) int {
	switch x := o.(type) {
	case nil:
		if i.isNull() {
			return 0
		}

		return 1
	case mavenInt:
		return compareDigitStrings(i.v, x.v)
	}

	return 1
}

func (s mavenStr) cmp(o mavenItem) int {
	switch x := o.(type) {
	case nil:
		return strings.Compare(s.comparable(), releaseIndex)
	case mavenStr:
		return strings.Compare(s.comparable(), x.comparable())
	}

	return -1
}

func (l *mavenList) cmp(o mavenItem) int {
	switch x := o.(type) {
	case nil:
		if len(l.items) == 0 {
			return 0
		}

		return l.items[0].cmp(nil)
	case mavenInt:
		return -1
	case mavenStr:
		return 1
	case *mavenList:
		for k := 0; k < len(l.items) || k < len(x.items); k++ {
			var li, ri mavenItem
			if k < len(l.items) {
				li = l.items[k]
			}

			if k < len(x.items) {
				ri = x.items[k]
			}

			var c int

			switch {
			case li == nil && ri == nil:
				c = 0
			case li == nil:
				c = -ri.cmp(nil)
			default:
				c = li.cmp(ri)
			}

			if c != 0 {
				return c
			}
		}
	}

	return 0
}

func (l *mavenList) normalize() {
	for i := len(l.items) - 1; i >= 0; i-- {
		last := l.items[i]

		if last.isNull() {
			l.items = slices.Delete(l.items, i, i+1)
		} else if _, nested := last.(*mavenList); !nested {
			break
		}
	}
}

func parseMavenItem(isNum bool, s string) mavenItem {
	if isNum {
		return newMavenInt(s)
	}

	return newMavenStr(s, false)
}

// parseMaven builds a ComparableVersion tree (Maven 3.9 semantics).
func parseMaven(v string) *mavenList {
	v = strings.ToLower(v)
	root := &mavenList{}
	list := root
	stack := []*mavenList{root}
	isNum := false
	start := 0

	push := func() {
		n := &mavenList{}
		list.items = append(list.items, n)
		list = n
		stack = append(stack, n)
	}

	add := func(end int) {
		if end == start {
			list.items = append(list.items, mavenInt{v: "0"})
		} else {
			list.items = append(list.items, parseMavenItem(isNum, v[start:end]))
		}
	}

	for i := range len(v) {
		c := v[i]

		switch {
		case c == '.':
			add(i)

			start = i + 1
		case c == '-':
			add(i)

			start = i + 1

			push()
		case isDigit(c):
			if !isNum && i > start {
				if len(list.items) > 0 {
					push()
				}

				list.items = append(list.items, newMavenStr(v[start:i], true))
				start = i

				push()
			}

			isNum = true
		default:
			if isNum && i > start {
				list.items = append(list.items, parseMavenItem(true, v[start:i]))
				start = i

				push()
			}

			isNum = false
		}
	}

	if len(v) > start {
		if !isNum && len(list.items) > 0 {
			push()
		}

		list.items = append(list.items, parseMavenItem(isNum, v[start:]))
	}

	for i := len(stack) - 1; i >= 0; i-- {
		stack[i].normalize()
	}

	return root
}
