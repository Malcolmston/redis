package redis

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"user:*", "user:1", true},
		{"user:*", "post:1", false},
		{"user:?", "user:1", true},
		{"user:?", "user:12", false},
		{"h[ae]llo", "hello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
		{"h[^e]llo", "hallo", true},
		{"h[^e]llo", "hello", false},
		{"[a-c]at", "bat", true},
		{"[a-c]at", "dat", false},
		{"foo\\*bar", "foo*bar", true},
		{"foo\\*bar", "fooXbar", false},
		{"a*c", "abbbc", true},
		{"a*c", "abbb", false},
		{"*end", "the end", true},
		{"pre*", "prefix", true},
		{"a*b*c", "axxbyyc", true},
		{"literal", "literal", true},
		{"literal", "literaX", false},
		// Malformed class treated literally.
		{"[abc", "[abc", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.name); got != c.want {
			t.Errorf("Match(%q,%q) = %v want %v", c.pattern, c.name, got, c.want)
		}
	}
}
