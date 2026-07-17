package redis

// Match reports whether name matches the glob-style pattern used by the KEYS
// command. Supported metacharacters:
//
//	"*"        matches any sequence of characters (including none)
//	"?"        matches any single character
//	"[abc]"    matches any character in the set
//	"[^abc]"   matches any character not in the set
//	"[a-z]"    matches any character in the range
//	"\x"       matches the literal character x
//
// Matching is performed over bytes.
func Match(pattern, name string) bool {
	return matchBytes(pattern, name)
}

func matchBytes(pattern, name string) bool {
	px, nx := 0, 0
	// Backtracking state for the most recent '*'.
	star, starName := -1, 0
	for nx < len(name) {
		if px < len(pattern) {
			switch c := pattern[px]; c {
			case '*':
				star = px
				starName = nx
				px++
				continue
			case '?':
				px++
				nx++
				continue
			case '[':
				if matched, next, ok := matchClass(pattern, px, name[nx]); ok {
					if matched {
						px = next
						nx++
						continue
					}
				} else {
					// Malformed class: treat '[' literally.
					if name[nx] == '[' {
						px++
						nx++
						continue
					}
				}
			case '\\':
				if px+1 < len(pattern) {
					if pattern[px+1] == name[nx] {
						px += 2
						nx++
						continue
					}
				} else if name[nx] == '\\' {
					px++
					nx++
					continue
				}
			default:
				if c == name[nx] {
					px++
					nx++
					continue
				}
			}
		}
		// Mismatch: backtrack to the last '*' if possible.
		if star != -1 {
			px = star + 1
			starName++
			nx = starName
			continue
		}
		return false
	}
	// Consume any trailing '*' in the pattern.
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// matchClass evaluates a bracket expression in pattern beginning at index i
// (pattern[i] == '['). It returns whether ch is in the class, the index just
// past the closing ']', and ok=false if the class is malformed (no ']').
func matchClass(pattern string, i int, ch byte) (matched bool, next int, ok bool) {
	j := i + 1
	negate := false
	if j < len(pattern) && pattern[j] == '^' {
		negate = true
		j++
	}
	found := false
	for j < len(pattern) && pattern[j] != ']' {
		if pattern[j] == '\\' && j+1 < len(pattern) {
			if pattern[j+1] == ch {
				found = true
			}
			j += 2
			continue
		}
		// Range a-z.
		if j+2 < len(pattern) && pattern[j+1] == '-' && pattern[j+2] != ']' {
			lo, hi := pattern[j], pattern[j+2]
			if lo <= ch && ch <= hi {
				found = true
			}
			j += 3
			continue
		}
		if pattern[j] == ch {
			found = true
		}
		j++
	}
	if j >= len(pattern) {
		return false, i, false // no closing bracket
	}
	// j points at ']'.
	return found != negate, j + 1, true
}
