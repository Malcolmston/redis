package redis

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Do executes a single command by name and arguments, returning a result in a
// RESP-friendly Go representation and an error for command-level failures.
// Command names are case-insensitive. The returned value uses these types:
//
//	SimpleString  for status replies such as OK
//	int64         for integer replies
//	string        for bulk-string replies
//	nil           for null replies (missing key, failed NX/XX)
//	[]any         for array replies
//
// Do is the in-process equivalent of a client sending a RESP command array.
func (s *Store) Do(args ...string) (any, error) {
	if len(args) == 0 {
		return nil, ErrWrongArgs
	}
	cmd := strings.ToUpper(args[0])
	a := args[1:]
	h, ok := dispatchTable[cmd]
	if !ok {
		return nil, fmt.Errorf("%w '%s'", ErrUnknownCommand, args[0])
	}
	return h(s, a)
}

type handler func(s *Store, a []string) (any, error)

var dispatchTable map[string]handler

func init() {
	dispatchTable = map[string]handler{
		// Strings.
		"SET":    cmdSet,
		"GET":    cmdGet,
		"GETSET": cmdGetSet,
		"APPEND": cmdAppend,
		"STRLEN": cmdStrlen,
		"INCR":   func(s *Store, a []string) (any, error) { return incrHelper(s, a, 1, false) },
		"DECR":   func(s *Store, a []string) (any, error) { return incrHelper(s, a, -1, false) },
		"INCRBY": func(s *Store, a []string) (any, error) { return incrHelper(s, a, 1, true) },
		"DECRBY": func(s *Store, a []string) (any, error) { return incrHelper(s, a, -1, true) },

		// Generic / keyspace.
		"DEL":     cmdDel,
		"EXISTS":  cmdExists,
		"EXPIRE":  cmdExpire,
		"PEXPIRE": cmdPExpire,
		"TTL":     func(s *Store, a []string) (any, error) { return ttlHelper(s, a, false) },
		"PTTL":    func(s *Store, a []string) (any, error) { return ttlHelper(s, a, true) },
		"PERSIST": cmdPersist,
		"KEYS":    cmdKeys,
		"TYPE":    cmdType,
		"DBSIZE":  cmdDBSize,
		"FLUSHALL": func(s *Store, a []string) (any, error) {
			s.FlushAll()
			return SimpleString("OK"), nil
		},

		// Lists.
		"LPUSH":  func(s *Store, a []string) (any, error) { return pushHelper(s, a, true) },
		"RPUSH":  func(s *Store, a []string) (any, error) { return pushHelper(s, a, false) },
		"LPOP":   func(s *Store, a []string) (any, error) { return popHelper(s, a, true) },
		"RPOP":   func(s *Store, a []string) (any, error) { return popHelper(s, a, false) },
		"LLEN":   cmdLLen,
		"LINDEX": cmdLIndex,
		"LRANGE": cmdLRange,

		// Hashes.
		"HSET":    cmdHSet,
		"HGET":    cmdHGet,
		"HDEL":    cmdHDel,
		"HEXISTS": cmdHExists,
		"HLEN":    cmdHLen,
		"HKEYS":   cmdHKeys,
		"HVALS":   cmdHVals,
		"HGETALL": cmdHGetAll,

		// Sets.
		"SADD":      cmdSAdd,
		"SREM":      cmdSRem,
		"SISMEMBER": cmdSIsMember,
		"SCARD":     cmdSCard,
		"SMEMBERS":  cmdSMembers,
		"SINTER":    func(s *Store, a []string) (any, error) { return setOpHelper(s, a, s.SInter) },
		"SUNION":    func(s *Store, a []string) (any, error) { return setOpHelper(s, a, s.SUnion) },
		"SDIFF":     func(s *Store, a []string) (any, error) { return setOpHelper(s, a, s.SDiff) },

		// Sorted sets.
		"ZADD":          cmdZAdd,
		"ZREM":          cmdZRem,
		"ZSCORE":        cmdZScore,
		"ZCARD":         cmdZCard,
		"ZRANK":         cmdZRank,
		"ZREVRANK":      cmdZRevRank,
		"ZRANGE":        func(s *Store, a []string) (any, error) { return zrangeHelper(s, a, false) },
		"ZREVRANGE":     func(s *Store, a []string) (any, error) { return zrangeHelper(s, a, true) },
		"ZRANGEBYSCORE": cmdZRangeByScore,
	}
}

func toStringSlice(members []string) []any {
	out := make([]any, len(members))
	for i, m := range members {
		out[i] = m
	}
	return out
}

// ---- strings ----

func cmdSet(s *Store, a []string) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	key, val := a[0], a[1]
	var opts SetOptions
	for i := 2; i < len(a); i++ {
		switch strings.ToUpper(a[i]) {
		case "NX":
			opts.NX = true
		case "XX":
			opts.XX = true
		case "EX":
			if i+1 >= len(a) {
				return nil, ErrSyntax
			}
			n, err := strconv.Atoi(a[i+1])
			if err != nil {
				return nil, ErrNotInteger
			}
			opts.EX = time.Duration(n) * time.Second
			i++
		case "PX":
			if i+1 >= len(a) {
				return nil, ErrSyntax
			}
			n, err := strconv.Atoi(a[i+1])
			if err != nil {
				return nil, ErrNotInteger
			}
			opts.PX = time.Duration(n) * time.Millisecond
			i++
		default:
			return nil, ErrSyntax
		}
	}
	if opts.NX && opts.XX {
		return nil, ErrSyntax
	}
	if s.Set(key, val, opts) {
		return SimpleString("OK"), nil
	}
	return nil, nil
}

func cmdGet(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	v, ok, err := s.Get(a[0])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return v, nil
}

func cmdGetSet(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	v, ok, err := s.GetSet(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return v, nil
}

func cmdAppend(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	n, err := s.Append(a[0], a[1])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdStrlen(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.Strlen(a[0])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func incrHelper(s *Store, a []string, sign int64, withArg bool) (any, error) {
	if withArg {
		if len(a) != 2 {
			return nil, ErrWrongArgs
		}
		delta, err := strconv.ParseInt(a[1], 10, 64)
		if err != nil {
			return nil, ErrNotInteger
		}
		n, err := s.IncrBy(a[0], sign*delta)
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.IncrBy(a[0], sign)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// ---- generic ----

func cmdDel(s *Store, a []string) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	return int64(s.Del(a...)), nil
}

func cmdExists(s *Store, a []string) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	return int64(s.Exists(a...)), nil
}

func cmdExpire(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	n, err := strconv.Atoi(a[1])
	if err != nil {
		return nil, ErrNotInteger
	}
	if s.Expire(a[0], time.Duration(n)*time.Second) {
		return int64(1), nil
	}
	return int64(0), nil
}

func cmdPExpire(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	n, err := strconv.Atoi(a[1])
	if err != nil {
		return nil, ErrNotInteger
	}
	if s.PExpire(a[0], time.Duration(n)*time.Millisecond) {
		return int64(1), nil
	}
	return int64(0), nil
}

func ttlHelper(s *Store, a []string, milli bool) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	d, code := s.TTL(a[0])
	switch code {
	case TTLNoKey:
		return int64(-2), nil
	case TTLNoExpiry:
		return int64(-1), nil
	default:
		if milli {
			return int64(d / time.Millisecond), nil
		}
		// Round up to whole seconds, matching Redis TTL.
		secs := (d + time.Second - 1) / time.Second
		return int64(secs), nil
	}
}

func cmdPersist(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	if s.Persist(a[0]) {
		return int64(1), nil
	}
	return int64(0), nil
}

func cmdKeys(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	return toStringSlice(s.Keys(a[0])), nil
}

func cmdType(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	return SimpleString(string(s.TypeOf(a[0]))), nil
}

func cmdDBSize(s *Store, a []string) (any, error) {
	if len(a) != 0 {
		return nil, ErrWrongArgs
	}
	return int64(s.DBSize()), nil
}

// ---- lists ----

func pushHelper(s *Store, a []string, left bool) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	var n int
	var err error
	if left {
		n, err = s.LPush(a[0], a[1:]...)
	} else {
		n, err = s.RPush(a[0], a[1:]...)
	}
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func popHelper(s *Store, a []string, left bool) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	var v string
	var ok bool
	var err error
	if left {
		v, ok, err = s.LPop(a[0])
	} else {
		v, ok, err = s.RPop(a[0])
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return v, nil
}

func cmdLLen(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.LLen(a[0])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdLIndex(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	idx, err := strconv.Atoi(a[1])
	if err != nil {
		return nil, ErrNotInteger
	}
	v, ok, err := s.LIndex(a[0], idx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return v, nil
}

func cmdLRange(s *Store, a []string) (any, error) {
	if len(a) != 3 {
		return nil, ErrWrongArgs
	}
	start, err1 := strconv.Atoi(a[1])
	stop, err2 := strconv.Atoi(a[2])
	if err1 != nil || err2 != nil {
		return nil, ErrNotInteger
	}
	vals, err := s.LRange(a[0], start, stop)
	if err != nil {
		return nil, err
	}
	return toStringSlice(vals), nil
}

// ---- hashes ----

func cmdHSet(s *Store, a []string) (any, error) {
	if len(a) < 3 || len(a)%2 != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.HSet(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdHGet(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	v, ok, err := s.HGet(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return v, nil
}

func cmdHDel(s *Store, a []string) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	n, err := s.HDel(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdHExists(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	ok, err := s.HExists(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if ok {
		return int64(1), nil
	}
	return int64(0), nil
}

func cmdHLen(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.HLen(a[0])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdHKeys(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	v, err := s.HKeys(a[0])
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

func cmdHVals(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	v, err := s.HVals(a[0])
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

func cmdHGetAll(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	m, err := s.HGetAll(a[0])
	if err != nil {
		return nil, err
	}
	keys, err := s.HKeys(a[0])
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		out = append(out, k, m[k])
	}
	return out, nil
}

// ---- sets ----

func cmdSAdd(s *Store, a []string) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	n, err := s.SAdd(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdSRem(s *Store, a []string) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	n, err := s.SRem(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdSIsMember(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	ok, err := s.SIsMember(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if ok {
		return int64(1), nil
	}
	return int64(0), nil
}

func cmdSCard(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.SCard(a[0])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdSMembers(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	v, err := s.SMembers(a[0])
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

func setOpHelper(s *Store, a []string, fn func(...string) ([]string, error)) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	v, err := fn(a...)
	if err != nil {
		return nil, err
	}
	return toStringSlice(v), nil
}

// ---- sorted sets ----

func cmdZAdd(s *Store, a []string) (any, error) {
	if len(a) < 3 || (len(a)-1)%2 != 0 {
		return nil, ErrWrongArgs
	}
	members := make([]ZMember, 0, (len(a)-1)/2)
	for i := 1; i < len(a); i += 2 {
		score, err := strconv.ParseFloat(a[i], 64)
		if err != nil {
			return nil, ErrNotFloat
		}
		members = append(members, ZMember{Member: a[i+1], Score: score})
	}
	n, err := s.ZAdd(a[0], members...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdZRem(s *Store, a []string) (any, error) {
	if len(a) < 2 {
		return nil, ErrWrongArgs
	}
	n, err := s.ZRem(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdZScore(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	sc, ok, err := s.ZScore(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return formatFloat(sc), nil
}

func cmdZCard(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.ZCard(a[0])
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func cmdZRank(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	r, ok, err := s.ZRank(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return int64(r), nil
}

func cmdZRevRank(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	r, ok, err := s.ZRevRank(a[0], a[1])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return int64(r), nil
}

func zrangeHelper(s *Store, a []string, rev bool) (any, error) {
	if len(a) < 3 {
		return nil, ErrWrongArgs
	}
	start, err1 := strconv.Atoi(a[1])
	stop, err2 := strconv.Atoi(a[2])
	if err1 != nil || err2 != nil {
		return nil, ErrNotInteger
	}
	withScores := false
	if len(a) == 4 && strings.EqualFold(a[3], "WITHSCORES") {
		withScores = true
	} else if len(a) > 3 {
		return nil, ErrSyntax
	}
	var members []ZMember
	var err error
	if rev {
		members, err = s.ZRevRange(a[0], start, stop)
	} else {
		members, err = s.ZRange(a[0], start, stop)
	}
	if err != nil {
		return nil, err
	}
	return zmembersReply(members, withScores), nil
}

func cmdZRangeByScore(s *Store, a []string) (any, error) {
	if len(a) < 3 {
		return nil, ErrWrongArgs
	}
	min, minEx, err := parseScoreBound(a[1])
	if err != nil {
		return nil, err
	}
	max, maxEx, err := parseScoreBound(a[2])
	if err != nil {
		return nil, err
	}
	withScores := false
	if len(a) == 4 && strings.EqualFold(a[3], "WITHSCORES") {
		withScores = true
	} else if len(a) > 3 {
		return nil, ErrSyntax
	}
	members, err := s.ZRangeByScore(a[0], ScoreRange{
		Min: min, Max: max, MinExclusive: minEx, MaxExclusive: maxEx,
	})
	if err != nil {
		return nil, err
	}
	return zmembersReply(members, withScores), nil
}

func zmembersReply(members []ZMember, withScores bool) []any {
	out := make([]any, 0, len(members))
	for _, m := range members {
		out = append(out, m.Member)
		if withScores {
			out = append(out, formatFloat(m.Score))
		}
	}
	return out
}

// parseScoreBound parses a ZRANGEBYSCORE bound, honoring the "(" exclusive
// prefix and the +inf/-inf keywords.
func parseScoreBound(s string) (val float64, exclusive bool, err error) {
	if strings.HasPrefix(s, "(") {
		exclusive = true
		s = s[1:]
	}
	switch strings.ToLower(s) {
	case "+inf", "inf":
		return math.Inf(1), exclusive, nil
	case "-inf":
		return math.Inf(-1), exclusive, nil
	}
	v, perr := strconv.ParseFloat(s, 64)
	if perr != nil {
		return 0, false, ErrNotFloat
	}
	return v, exclusive, nil
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
