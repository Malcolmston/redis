package redis

import "time"

// GetDel atomically returns the string value stored at key and deletes the key.
// The boolean is false when the key is absent or expired, in which case nothing
// is deleted. It returns ErrWrongType if the key holds a non-string value,
// mirroring the Redis GETDEL command.
func (s *Store) GetDel(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", false, nil
	}
	if it.kind != TypeString {
		return "", false, ErrWrongType
	}
	v := it.str
	delete(s.data, key)
	return v, true, nil
}

// GetExOptions controls how GetEx adjusts the expiration of the fetched key.
// The zero value leaves any existing TTL untouched. At most one of EX, PX, or
// Persist should be set; if several are set, PX takes precedence over EX, and
// Persist takes precedence over both.
type GetExOptions struct {
	// EX sets a new expiration in seconds. Ignored if zero.
	EX time.Duration
	// PX sets a new expiration in milliseconds. Ignored if zero.
	PX time.Duration
	// Persist removes any existing expiration, making the key persistent.
	Persist bool
}

// GetEx returns the string value stored at key while optionally modifying its
// expiration according to opts. The boolean is false when the key is absent or
// expired. It returns ErrWrongType if the key holds a non-string value,
// mirroring the Redis GETEX command.
func (s *Store) GetEx(key string, opts GetExOptions) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", false, nil
	}
	if it.kind != TypeString {
		return "", false, ErrWrongType
	}
	switch {
	case opts.Persist:
		it.expireAt = time.Time{}
	case opts.PX > 0:
		it.expireAt = s.now().Add(opts.PX)
	case opts.EX > 0:
		it.expireAt = s.now().Add(opts.EX)
	}
	return it.str, true, nil
}

// SubStr returns the substring of the value stored at key between the inclusive
// byte offsets start and stop. Negative offsets count back from the end of the
// string (-1 is the last byte). Out-of-range offsets are clamped. It is the
// classic alias of GetRange and mirrors the Redis SUBSTR command. It returns
// ErrWrongType if the key holds a non-string value.
func (s *Store) SubStr(key string, start, stop int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", nil
	}
	if it.kind != TypeString {
		return "", ErrWrongType
	}
	lo, hi, ok := normalizeRange(start, stop, len(it.str))
	if !ok {
		return "", nil
	}
	return it.str[lo : hi+1], nil
}

// Lcs returns the longest common subsequence of the string values stored at
// key1 and key2. A missing key is treated as an empty string. It returns
// ErrWrongType if either key holds a non-string value, mirroring the Redis LCS
// command with no extra options.
func (s *Store) Lcs(key1, key2 string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, b, err := s.stringsextLCSInputs(key1, key2)
	if err != nil {
		return "", err
	}
	_, seq := stringsextLCS(a, b)
	return seq, nil
}

// LcsLen returns the length of the longest common subsequence of the string
// values stored at key1 and key2. A missing key is treated as an empty string.
// It returns ErrWrongType if either key holds a non-string value, mirroring the
// Redis LCS ... LEN command.
func (s *Store) LcsLen(key1, key2 string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, b, err := s.stringsextLCSInputs(key1, key2)
	if err != nil {
		return 0, err
	}
	n, _ := stringsextLCS(a, b)
	return n, nil
}

// LcsMatch describes one contiguous run shared by two strings, as reported by
// LcsIdx. The run spans key1's value at byte offsets [AStart, AEnd] and key2's
// value at byte offsets [BStart, BEnd]; both ranges are inclusive and have the
// same length, given by Len.
type LcsMatch struct {
	// AStart and AEnd are the inclusive byte offsets of the run in key1.
	AStart, AEnd int
	// BStart and BEnd are the inclusive byte offsets of the run in key2.
	BStart, BEnd int
	// Len is the length of the run in bytes.
	Len int
}

// LcsIdx returns the contiguous matching runs that make up the longest common
// subsequence of the string values stored at key1 and key2, together with the
// total length of the subsequence. Matches are ordered from the highest offsets
// to the lowest, matching the Redis LCS ... IDX WITHMATCHLEN reply. A missing
// key is treated as an empty string. It returns ErrWrongType if either key
// holds a non-string value.
func (s *Store) LcsIdx(key1, key2 string) ([]LcsMatch, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, b, err := s.stringsextLCSInputs(key1, key2)
	if err != nil {
		return nil, 0, err
	}
	matches, total := stringsextLCSIdx(a, b)
	return matches, total, nil
}

// stringsextLCSInputs fetches the string values at key1 and key2. Callers must
// hold mu. A missing key yields an empty string; a non-string value is an error.
func (s *Store) stringsextLCSInputs(key1, key2 string) (string, string, error) {
	var a, b string
	if it := s.getLive(key1); it != nil {
		if it.kind != TypeString {
			return "", "", ErrWrongType
		}
		a = it.str
	}
	if it := s.getLive(key2); it != nil {
		if it.kind != TypeString {
			return "", "", ErrWrongType
		}
		b = it.str
	}
	return a, b, nil
}

// stringsextLCSTable builds the classic longest-common-subsequence dynamic
// programming table for a and b. Cell [i][j] holds the LCS length of a[:i] and
// b[:j].
func stringsextLCSTable(a, b string) [][]int {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

// stringsextLCS returns the length and the actual longest common subsequence of
// a and b.
func stringsextLCS(a, b string) (int, string) {
	dp := stringsextLCSTable(a, b)
	i, j := len(a), len(b)
	buf := make([]byte, 0, dp[i][j])
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			buf = append(buf, a[i-1])
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	// buf was built from the tail, so reverse it.
	for l, r := 0, len(buf)-1; l < r; l, r = l+1, r-1 {
		buf[l], buf[r] = buf[r], buf[l]
	}
	return dp[len(a)][len(b)], string(buf)
}

// stringsextLCSIdx returns the contiguous matching runs and total length of the
// longest common subsequence of a and b, ordered from highest to lowest offset.
func stringsextLCSIdx(a, b string) ([]LcsMatch, int) {
	dp := stringsextLCSTable(a, b)
	i, j := len(a), len(b)
	total := dp[i][j]
	var matches []LcsMatch
	var run *LcsMatch
	flush := func() {
		if run != nil {
			matches = append(matches, *run)
			run = nil
		}
	}
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			ai, bj := i-1, j-1
			if run != nil && run.AStart == ai+1 && run.BStart == bj+1 {
				run.AStart = ai
				run.BStart = bj
				run.Len++
			} else {
				flush()
				run = &LcsMatch{AStart: ai, AEnd: ai, BStart: bj, BEnd: bj, Len: 1}
			}
			i--
			j--
		} else {
			flush()
			if dp[i-1][j] >= dp[i][j-1] {
				i--
			} else {
				j--
			}
		}
	}
	flush()
	return matches, total
}
