package search

import "testing"

func TestDetectQueryType(t *testing.T) {
	cases := []struct {
		q    string
		want QueryType
	}{
		{"UserAuth", QueryBM25Heavy},     // camelCase
		{"get_user_id", QueryBM25Heavy},  // snake_case
		{"foo::bar", QueryBM25Heavy},     // scope
		{"obj.method()", QueryBM25Heavy}, // method call
		{"main.go", QueryBM25Heavy},      // file ext
		{"CC-1234", QueryBM25Heavy},      // ticket
		{"auth bug", QueryHybrid},        // 2 words, no identifier
		{"como autenticar usuários no NestJS via JWT", QueryDenseHeavy}, // prose
		{"", QueryHybrid},
	}
	for _, c := range cases {
		if got := DetectQueryType(c.q); got != c.want {
			t.Errorf("DetectQueryType(%q) = %d, want %d", c.q, got, c.want)
		}
	}
}

func TestMergeRRF_EmptyRankings(t *testing.T) {
	got := MergeRRF(nil, nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestMergeRRF_SingleSource(t *testing.T) {
	rankings := map[string][]string{
		"bm25": {"a", "b", "c"},
	}
	got := MergeRRF(rankings, nil)
	if len(got) != 3 {
		t.Fatalf("want 3 hits, got %d", len(got))
	}
	if got[0].SessionID != "a" || got[1].SessionID != "b" || got[2].SessionID != "c" {
		t.Errorf("unexpected order: %+v", got)
	}
	// score(a) = 1/(60+1) > score(b) = 1/(60+2)
	if got[0].Score <= got[1].Score {
		t.Errorf("expected a > b, got %v", got)
	}
}

func TestMergeRRF_OverlappingSources(t *testing.T) {
	// `a` aparece em ambas fontes → score maior que `b` que só em uma
	rankings := map[string][]string{
		"bm25":  {"a", "b"},
		"dense": {"a", "c"},
	}
	got := MergeRRF(rankings, nil)
	if got[0].SessionID != "a" {
		t.Errorf("expected 'a' to win (in both), got %q", got[0].SessionID)
	}
	expected := 2.0 / 61.0 // 1/61 + 1/61
	if got[0].Score < expected-0.0001 || got[0].Score > expected+0.0001 {
		t.Errorf("score(a) = %v, want ~%v", got[0].Score, expected)
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("expected 2 sources for 'a', got %v", got[0].Sources)
	}
}

func TestMergeRRF_WeightsBM25Heavy(t *testing.T) {
	// `b` rankeia melhor em BM25, `c` melhor em dense.
	// Com BM25-heavy weights, `b` deve vencer.
	rankings := map[string][]string{
		"bm25":  {"b", "x"},
		"dense": {"c", "x"},
	}
	got := MergeRRF(rankings, WeightsFor(QueryBM25Heavy))
	// `b` ganha 1.5/61 = 0.02459
	// `c` ganha 0.5/61 = 0.00819
	// `x` ganha 1.5/62 + 0.5/62 = 2.0/62 = 0.03226 (em ambas, rank 2)
	// Então x > b > c
	if got[0].SessionID != "x" {
		t.Errorf("expected 'x' (in both at rank 2), got %q", got[0].SessionID)
	}
	if got[1].SessionID != "b" {
		t.Errorf("expected 'b' (BM25 weight 1.5) > 'c' (dense weight 0.5), got %q", got[1].SessionID)
	}
}

func TestMergeRRF_TieBreakerStable(t *testing.T) {
	// 2 ids com ranks idênticos em fontes idênticas → tiebreak por id asc
	rankings := map[string][]string{
		"bm25": {"zzz", "aaa"},
	}
	got := MergeRRF(rankings, nil)
	// rank diferente, então score diferente — não testa tie aqui
	if got[0].SessionID != "zzz" || got[1].SessionID != "aaa" {
		t.Errorf("expected zzz > aaa by rank, got %v", got)
	}

	// Tie real: ambos rank 1 em fontes diferentes
	rankings2 := map[string][]string{
		"bm25":  {"zzz"},
		"dense": {"aaa"},
	}
	got2 := MergeRRF(rankings2, nil)
	// Mesmo score (1/61) em fontes diferentes → tiebreak por id
	if got2[0].SessionID != "aaa" || got2[1].SessionID != "zzz" {
		t.Errorf("expected aaa < zzz tiebreak, got %v", got2)
	}
}
