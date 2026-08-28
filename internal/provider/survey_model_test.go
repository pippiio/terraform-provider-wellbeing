package provider

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDeriveQuestionKeyLowercasesAndSeparates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain ascii", input: "How are you today?", want: "how-are-you-today"},
		{name: "uppercase is folded", input: "HVAD SYNES DU?", want: "hvad-synes-du"},
		{name: "digits are kept", input: "10 - Meget sandsynligt", want: "10-meget-sandsynligt"},
		{name: "repeated separators collapse", input: "a  --  b", want: "a-b"},
		{name: "leading and trailing punctuation is trimmed", input: "  ?Hej!  ", want: "hej"},
		{name: "punctuation only yields empty", input: "???", want: ""},
		{name: "empty yields empty", input: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveQuestionKey(test.input); got != test.want {
				t.Errorf("deriveQuestionKey(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestDeriveQuestionKeyFoldsDanishLetters(t *testing.T) {
	// Danish is the primary survey language, so æ/ø/å must fold to readable
	// digraphs rather than collapsing to separators.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "oe lowercase", input: "Føler du dig tilpas?", want: "foeler-du-dig-tilpas"},
		{name: "aa lowercase", input: "Hvordan går det på arbejdet?", want: "hvordan-gaar-det-paa-arbejdet"},
		{name: "ae lowercase", input: "Er der ærlig kommunikation?", want: "er-der-aerlig-kommunikation"},
		{name: "uppercase danish letters", input: "ÆØÅ", want: "aeoeaa"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveQuestionKey(test.input); got != test.want {
				t.Errorf("deriveQuestionKey(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestDeriveQuestionKeyDegradesOtherDiacritics(t *testing.T) {
	// Documented limitation: only Danish letters fold. Any other non-ASCII rune
	// becomes a separator, which can create collisions — those are caught at
	// plan time by detectQuestionKeyCollisions rather than silently accepted.
	if got := deriveQuestionKey("Café"); got != "caf" {
		t.Errorf("deriveQuestionKey(%q) = %q, want %q", "Café", got, "caf")
	}
}

func TestDeriveQuestionKeyTruncatesLongText(t *testing.T) {
	// The longest real question text runs to 200 characters, so truncation is
	// the normal case, not an edge case.
	long := strings.Repeat("a", 80)

	got := deriveQuestionKey(long)

	if len(got) != maxQuestionKeyLength {
		t.Fatalf("len = %d, want %d", len(got), maxQuestionKeyLength)
	}
	if got != strings.Repeat("a", maxQuestionKeyLength) {
		t.Errorf("got %q, want %d repeated a", got, maxQuestionKeyLength)
	}
}

func TestDeriveQuestionKeyTruncationLeavesNoTrailingSeparator(t *testing.T) {
	// Cutting mid-word must not leave a dangling separator, which would read as
	// a different key than the same text truncated one character earlier.
	input := strings.Repeat("a", maxQuestionKeyLength-1) + " bbbb"

	got := deriveQuestionKey(input)

	if strings.HasSuffix(got, "-") {
		t.Errorf("got %q, want no trailing separator", got)
	}
	if got != strings.Repeat("a", maxQuestionKeyLength-1) {
		t.Errorf("got %q, want %d repeated a", got, maxQuestionKeyLength-1)
	}
}

func TestDetectQuestionKeyCollisionsFindsNoneWhenUnique(t *testing.T) {
	keys := []string{"foerste", "anden", "tredje"}

	if got := detectQuestionKeyCollisions(keys); len(got) != 0 {
		t.Errorf("detectQuestionKeyCollisions(%v) = %v, want none", keys, got)
	}
}

func TestDetectQuestionKeyCollisionsReportsEveryIndex(t *testing.T) {
	keys := []string{"trivsel", "arbejdsmiljoe", "trivsel", "loen", "trivsel"}

	got := detectQuestionKeyCollisions(keys)

	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1: %v", len(got), got)
	}
	if got[0].Key != "trivsel" {
		t.Errorf("Key = %q, want %q", got[0].Key, "trivsel")
	}
	if len(got[0].Indices) != 3 {
		t.Fatalf("Indices = %v, want 3 entries", got[0].Indices)
	}
	for i, want := range []int{0, 2, 4} {
		if got[0].Indices[i] != want {
			t.Errorf("Indices[%d] = %d, want %d", i, got[0].Indices[i], want)
		}
	}
}

func TestDetectQuestionKeyCollisionsIsOrderedByKey(t *testing.T) {
	// Stable output keeps diagnostics identical across runs; Go map iteration
	// order is deliberately randomised.
	keys := []string{"zulu", "alpha", "zulu", "alpha"}

	got := detectQuestionKeyCollisions(keys)

	if len(got) != 2 {
		t.Fatalf("got %d collisions, want 2", len(got))
	}
	if got[0].Key != "alpha" || got[1].Key != "zulu" {
		t.Errorf("keys = %q, %q; want alpha, zulu", got[0].Key, got[1].Key)
	}
}

func TestDetectQuestionKeyCollisionsIgnoresEmptyKeys(t *testing.T) {
	// An empty key means the text derived nothing usable. That is reported as a
	// missing-key error elsewhere, not as a collision between two blanks.
	keys := []string{"", "trivsel", ""}

	if got := detectQuestionKeyCollisions(keys); len(got) != 0 {
		t.Errorf("detectQuestionKeyCollisions(%v) = %v, want none", keys, got)
	}
}

// duplicateHeavySurvey mirrors the shape of a real survey that carries five
// pairs of identically-worded questions: 25 questions, five of which repeat.
// The wording is fictional; only the structure is taken from real data.
func duplicateHeavySurvey() []string {
	texts := make([]string, 0, 25)
	for i := range 20 {
		texts = append(texts, "Unikt spoergsmaal nummer "+string(rune('a'+i)))
	}
	repeated := []string{
		"Føler du dig tilpas i tonen på arbejdspladsen?",
		"Har du oplevet uklare forventninger til din rolle?",
		"Oplever du at have indflydelse på dine opgaver?",
		"Får du den støtte du har brug for fra din leder?",
		"Er arbejdsmængden passende i forhold til din tid?",
	}
	// Each repeated text appears twice: once here and once appended below.
	texts = append(texts, repeated...)
	return append(texts[:20:20], append(repeated, repeated...)...)
}

func TestDetectQuestionKeyCollisionsOnDuplicateHeavySurvey(t *testing.T) {
	texts := duplicateHeavySurvey()

	keys := make([]string, 0, len(texts))
	for _, text := range texts {
		keys = append(keys, deriveQuestionKey(text))
	}

	got := detectQuestionKeyCollisions(keys)

	if len(got) != 5 {
		t.Fatalf("got %d collisions, want 5: %v", len(got), got)
	}
	for _, collision := range got {
		if len(collision.Indices) != 2 {
			t.Errorf("collision %q has %d indices, want 2", collision.Key, len(collision.Indices))
		}
	}
}

func TestQuestionKeyDiagnosticsNamesIndicesAndTexts(t *testing.T) {
	keys := []string{"trivsel", "loen", "trivsel"}
	texts := []string{"Er du glad?", "Er lønnen fair?", "Er du glad igen?"}

	diags := questionKeyDiagnostics(keys, texts)

	if !diags.HasError() {
		t.Fatal("expected an error diagnostic for the duplicate key")
	}
	if len(diags.Errors()) != 1 {
		t.Fatalf("got %d errors, want 1", len(diags.Errors()))
	}

	detail := diags.Errors()[0].Detail()
	for _, want := range []string{"trivsel", "question[0]", "question[2]", "Er du glad?", "Er du glad igen?", "key"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q does not mention %q", detail, want)
		}
	}
	// The block that is fine must not be dragged into the message.
	if strings.Contains(detail, "Er lønnen fair?") {
		t.Errorf("detail %q mentions a non-colliding question", detail)
	}
}

func TestQuestionKeyDiagnosticsIsSilentWhenUnique(t *testing.T) {
	keys := []string{"trivsel", "loen"}
	texts := []string{"Er du glad?", "Er lønnen fair?"}

	if diags := questionKeyDiagnostics(keys, texts); diags.HasError() {
		t.Errorf("expected no diagnostics, got %v", diags)
	}
}

// Guards the byte-slice truncation in deriveQuestionKey: the output alphabet is
// ASCII-only by construction, so slicing cannot split a rune. If a future
// folding rule emits a multi-byte rune this fails loudly.
func TestDeriveQuestionKeyOutputIsAlwaysValidASCII(t *testing.T) {
	inputs := []string{
		strings.Repeat("æøå", 60),
		strings.Repeat("Café ", 40),
		strings.Repeat("日本語のテキスト ", 20),
		strings.Repeat("Ærlig følelse på arbejdspladsen ", 10),
	}

	for _, input := range inputs {
		got := deriveQuestionKey(input)
		if !utf8.ValidString(got) {
			t.Errorf("deriveQuestionKey(%.20q...) produced invalid UTF-8: %q", input, got)
		}
		for i, r := range got {
			if r > 127 {
				t.Errorf("deriveQuestionKey(%.20q...) byte %d is non-ASCII rune %q", input, i, r)
			}
		}
		if len(got) > maxQuestionKeyLength {
			t.Errorf("deriveQuestionKey(%.20q...) len = %d, exceeds %d", input, len(got), maxQuestionKeyLength)
		}
	}
}
