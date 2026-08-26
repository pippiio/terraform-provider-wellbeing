package provider

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Story: Survey question identity
//
// Input:  the configured text of each `question` block in a wellbeing_survey
//         resource, plus any explicitly-set `key`.
// Process:
//   1. Derive a slug from the question text when no explicit key is given:
//      fold Danish letters, lowercase, replace anything else with a separator,
//      collapse repeats, trim, truncate.
//   2. Collect the effective key of every block, in block order.
//   3. Detect keys claimed by more than one block.
//   4. Render each collision as a Terraform diagnostic naming the offending
//      block indices and their texts.
// Output: either a stable key per question, or a plan-time error telling the
//         user exactly which blocks need an explicit `key`.
//
// Dependencies: none — pure functions, no I/O, no API calls.
// Side effects: none.
//
// Why keys matter: the Wellbeing API assigns its own Key/LabelKey to questions.
// Terraform needs a stable handle to match a configured block to a returned one
// across updates, or reordering and inserting would silently rewrite the wrong
// question. This is the same problem employee.id solves for the roster.

// maxQuestionKeyLength bounds a derived key. Real question texts run to 200
// characters, so truncation is the normal case. Truncation can itself create
// collisions, which is precisely what detectQuestionKeyCollisions catches.
const maxQuestionKeyLength = 64

// questionKeyCollision is one key claimed by more than one question block.
type questionKeyCollision struct {
	Key     string
	Indices []int
}

// danishFoldings are the letters that must survive slugging as readable
// digraphs. Danish is the primary survey language, so folding æ to "-" the way
// every other non-ASCII rune is folded would mangle most real question texts.
//
// Deliberately not a general Unicode normalisation: that would mean promoting
// golang.org/x/text to a direct dependency, and the provider keeps its
// dependencies inside the HashiCorp ecosystem. Any other diacritic degrades to
// a separator, which detectQuestionKeyCollisions turns into a plan-time error
// rather than a silent mismatch.
var danishFoldings = map[rune]string{
	'æ': "ae", 'Æ': "ae",
	'ø': "oe", 'Ø': "oe",
	'å': "aa", 'Å': "aa",
}

// deriveQuestionKey builds a stable slug from a question's text.
//
// Returns the empty string when the text carries no sluggable characters; the
// caller reports that as a missing key rather than treating it as valid.
func deriveQuestionKey(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	for _, r := range strings.ToLower(text) {
		if folded, ok := danishFoldings[r]; ok {
			b.WriteString(folded)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(questionKeySeparator)
	}

	slug := collapseSeparators(b.String())

	if len(slug) > maxQuestionKeyLength {
		slug = slug[:maxQuestionKeyLength]
	}
	// Truncation can re-expose a separator at the boundary; a trailing one would
	// read as a different key than the same text cut one character earlier.
	return strings.Trim(slug, string(questionKeySeparator))
}

const questionKeySeparator = '-'

// collapseSeparators squeezes runs of separators to one and trims the ends.
func collapseSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	previousWasSeparator := false
	for _, r := range s {
		if r == questionKeySeparator {
			previousWasSeparator = true
			continue
		}
		if previousWasSeparator && b.Len() > 0 {
			b.WriteByte(questionKeySeparator)
		}
		previousWasSeparator = false
		b.WriteRune(r)
	}

	return b.String()
}

// detectQuestionKeyCollisions reports every key used by more than one block.
//
// Empty keys are skipped: an empty key means the text derived nothing usable,
// which is a missing-key error, not a collision between two blanks.
//
// Results are ordered by key so diagnostics are identical across runs; Go map
// iteration order is deliberately randomised.
func detectQuestionKeyCollisions(keys []string) []questionKeyCollision {
	indicesByKey := make(map[string][]int, len(keys))
	for index, key := range keys {
		if key == "" {
			continue
		}
		indicesByKey[key] = append(indicesByKey[key], index)
	}

	collisions := make([]questionKeyCollision, 0)
	for _, key := range slices.Sorted(maps.Keys(indicesByKey)) {
		if len(indicesByKey[key]) < 2 {
			continue
		}
		collisions = append(collisions, questionKeyCollision{Key: key, Indices: indicesByKey[key]})
	}

	return collisions
}

// questionKeyDiagnostics renders collisions as Terraform errors.
//
// One diagnostic per colliding key, naming every offending block index and its
// text, so the fix is obvious without counting blocks by hand. Mirrors the
// duplicate-employee-id error on the roster resource.
func questionKeyDiagnostics(keys, texts []string) diag.Diagnostics {
	var diags diag.Diagnostics

	for _, collision := range detectQuestionKeyCollisions(keys) {
		offenders := make([]string, 0, len(collision.Indices))
		for _, index := range collision.Indices {
			text := ""
			if index < len(texts) {
				text = texts[index]
			}
			offenders = append(offenders, fmt.Sprintf("  question[%d]: %q", index, text))
		}

		diags.AddError(
			"Duplicate question key",
			fmt.Sprintf("These question blocks all resolve to the key %q:\n\n%s\n\n"+
				"A question's key is how Terraform matches it to the survey held by Wellbeing, "+
				"so it must be unique within the survey. When no key is set one is derived from "+
				"the question text, and identically-worded questions therefore collide.\n\n"+
				"Set an explicit key on all but one of the blocks above.",
				collision.Key, strings.Join(offenders, "\n")),
		)
	}

	return diags
}
