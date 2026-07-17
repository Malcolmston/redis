package redis

import "math/rand"

// A zset is a sorted set: a mapping from member to score plus a skiplist
// ordering members by (score, member). It supports logarithmic insertion,
// deletion, and rank lookup with correct score-then-lexicographic ordering.
type zset struct {
	dict map[string]float64
	sl   *skiplist
}

func newZSet() *zset {
	return &zset{dict: make(map[string]float64), sl: newSkiplist()}
}

func (z *zset) len() int { return len(z.dict) }

// add inserts or updates member with score, returning true if the member was
// newly added.
func (z *zset) add(member string, score float64) bool {
	if old, ok := z.dict[member]; ok {
		if old != score {
			z.sl.delete(old, member)
			z.sl.insert(score, member)
		}
		z.dict[member] = score
		return false
	}
	z.dict[member] = score
	z.sl.insert(score, member)
	return true
}

// remove deletes member, returning true if it was present.
func (z *zset) remove(member string) bool {
	score, ok := z.dict[member]
	if !ok {
		return false
	}
	delete(z.dict, member)
	z.sl.delete(score, member)
	return true
}

// score returns the score of member.
func (z *zset) score(member string) (float64, bool) {
	sc, ok := z.dict[member]
	return sc, ok
}

// rank returns the zero-based rank of member in ascending order.
func (z *zset) rank(member string) (int, bool) {
	score, ok := z.dict[member]
	if !ok {
		return 0, false
	}
	return z.sl.rank(score, member), true
}

const (
	skiplistMaxLevel = 32
	skiplistP        = 0.25
)

// slNode is a single skiplist node. Each level records the span (number of
// nodes) it skips to support O(log n) rank queries.
type slNode struct {
	score   float64
	member  string
	forward []*slNode
	span    []int
}

// skiplist is a probabilistic ordered structure keyed by (score, member).
type skiplist struct {
	head   *slNode
	level  int
	length int
	rng    *rand.Rand
}

func newSkiplist() *skiplist {
	return &skiplist{
		head:  &slNode{forward: make([]*slNode, skiplistMaxLevel), span: make([]int, skiplistMaxLevel)},
		level: 1,
		// Deterministic source keeps ordering reproducible; correctness does
		// not depend on the seed.
		rng: rand.New(rand.NewSource(1)),
	}
}

// less reports whether (s1, m1) orders before (s2, m2): score first, then
// member lexicographically.
func less(s1 float64, m1 string, s2 float64, m2 string) bool {
	if s1 != s2 {
		return s1 < s2
	}
	return m1 < m2
}

func (sl *skiplist) randomLevel() int {
	lvl := 1
	for lvl < skiplistMaxLevel && sl.rng.Float64() < skiplistP {
		lvl++
	}
	return lvl
}

// insert adds a new (score, member) node. The pair must not already exist.
func (sl *skiplist) insert(score float64, member string) {
	update := make([]*slNode, skiplistMaxLevel)
	rank := make([]int, skiplistMaxLevel)
	x := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		if i == sl.level-1 {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}
		for x.forward[i] != nil && less(x.forward[i].score, x.forward[i].member, score, member) {
			rank[i] += x.span[i]
			x = x.forward[i]
		}
		update[i] = x
	}

	lvl := sl.randomLevel()
	if lvl > sl.level {
		for i := sl.level; i < lvl; i++ {
			rank[i] = 0
			update[i] = sl.head
			update[i].span[i] = sl.length
		}
		sl.level = lvl
	}

	x = &slNode{score: score, member: member, forward: make([]*slNode, lvl), span: make([]int, lvl)}
	for i := 0; i < lvl; i++ {
		x.forward[i] = update[i].forward[i]
		update[i].forward[i] = x
		x.span[i] = update[i].span[i] - (rank[0] - rank[i])
		update[i].span[i] = (rank[0] - rank[i]) + 1
	}
	for i := lvl; i < sl.level; i++ {
		update[i].span[i]++
	}
	sl.length++
}

// delete removes the node matching (score, member). It is a no-op if absent.
func (sl *skiplist) delete(score float64, member string) {
	update := make([]*slNode, skiplistMaxLevel)
	x := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for x.forward[i] != nil && less(x.forward[i].score, x.forward[i].member, score, member) {
			x = x.forward[i]
		}
		update[i] = x
	}
	x = x.forward[0]
	if x == nil || x.score != score || x.member != member {
		return
	}
	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] == x {
			update[i].span[i] += x.span[i] - 1
			update[i].forward[i] = x.forward[i]
		} else {
			update[i].span[i]--
		}
	}
	for sl.level > 1 && sl.head.forward[sl.level-1] == nil {
		sl.level--
	}
	sl.length--
}

// rank returns the zero-based position of (score, member) in ascending order.
func (sl *skiplist) rank(score float64, member string) int {
	var r int
	x := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for x.forward[i] != nil &&
			(less(x.forward[i].score, x.forward[i].member, score, member) ||
				(x.forward[i].score == score && x.forward[i].member == member)) {
			r += x.span[i]
			x = x.forward[i]
			if x.score == score && x.member == member {
				return r - 1
			}
		}
	}
	return r - 1
}

// toSlice returns all members in ascending (score, member) order.
func (sl *skiplist) toSlice() []zmember {
	out := make([]zmember, 0, sl.length)
	for x := sl.head.forward[0]; x != nil; x = x.forward[0] {
		out = append(out, zmember{Member: x.member, Score: x.score})
	}
	return out
}

// zmember pairs a sorted-set member with its score.
type zmember struct {
	Member string
	Score  float64
}
