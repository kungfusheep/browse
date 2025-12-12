package sites

import "testing"

func TestLookup(t *testing.T) {
	// Should find known domains
	if info, ok := Lookup("lwn.net"); !ok {
		t.Error("lwn.net should be known")
	} else if info.Score < 100 {
		t.Errorf("lwn.net should have high score, got %d", info.Score)
	}

	// Should not find random domains
	if _, ok := Lookup("definitely-not-a-real-domain-12345.com"); ok {
		t.Error("random domain should not be known")
	}
}

func TestIsKnownGood(t *testing.T) {
	if !IsKnownGood("simonwillison.net") {
		t.Error("simonwillison.net should be known good")
	}
	if IsKnownGood("not-in-database.invalid") {
		t.Error("invalid domain should not be known good")
	}
}

func TestScore(t *testing.T) {
	if s := Score("en.wikipedia.org"); s == 0 {
		t.Error("wikipedia should have a score")
	}
	if s := Score("unknown-domain.test"); s != 0 {
		t.Error("unknown domain should return 0")
	}
}

func BenchmarkLookup(b *testing.B) {
	domains := []string{
		"lwn.net",
		"simonwillison.net",
		"unknown-domain.com",
		"en.wikipedia.org",
		"lobste.rs",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Lookup(domains[i%len(domains)])
	}
}
