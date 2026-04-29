package stats

// Stopwords minimalistas pt-BR + en. Não pretende ser exaustivo — pega o
// chão comum pra que palavras "interessantes" emerjam no top-N.
var stopwords = map[string]bool{}

func init() {
	for _, w := range stopwordsPT {
		stopwords[w] = true
	}
	for _, w := range stopwordsEN {
		stopwords[w] = true
	}
}

var stopwordsPT = []string{
	"a", "o", "as", "os", "um", "uma", "uns", "umas",
	"de", "do", "da", "dos", "das", "no", "na", "nos", "nas",
	"em", "com", "por", "para", "pra", "pro", "ao", "aos", "à", "às",
	"e", "ou", "mas", "que", "se", "como", "quando", "onde", "porque", "porém",
	"é", "são", "foi", "era", "ser", "estar", "está", "tá", "ter", "tem", "tinha",
	"eu", "tu", "ele", "ela", "nós", "vocês", "eles", "elas", "voce", "você",
	"meu", "minha", "seu", "sua", "nosso", "nossa", "deles", "delas",
	"isso", "isto", "aquilo", "esse", "essa", "este", "esta", "aquele", "aquela",
	"aqui", "aí", "ali", "lá",
	"não", "nao", "sim", "também", "tambem", "já", "ja", "ainda", "só", "so", "mais", "menos",
	"agora", "antes", "depois", "hoje", "ontem", "amanhã", "amanha",
	"muito", "muita", "muitos", "muitas", "pouco", "pouca", "poucos", "poucas",
	"todo", "toda", "todos", "todas", "outro", "outra", "outros", "outras",
	"vai", "vão", "ir", "vou", "vamos", "vem", "veio", "fiz", "fazer", "faz",
	"pode", "podem", "posso", "deve", "devem",
}

var stopwordsEN = []string{
	"a", "an", "the", "and", "or", "but", "if", "of", "in", "on", "at", "by", "for", "to",
	"is", "are", "was", "were", "be", "been", "being", "have", "has", "had",
	"i", "you", "he", "she", "it", "we", "they", "my", "your", "his", "her", "our", "their",
	"this", "that", "these", "those",
	"do", "does", "did", "doing",
	"so", "as", "with", "from", "up", "down", "out", "over", "under",
	"can", "could", "should", "would", "will", "may", "might",
	"not", "no", "yes",
	"me", "us", "them", "him", "what", "which", "who", "when", "where", "why", "how",
}

// IsStopword retorna true se a palavra (lowercase) está na lista.
func IsStopword(w string) bool {
	return stopwords[w]
}
