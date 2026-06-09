package main

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildFactsIncludesFactFamilies(t *testing.T) {
	facts := BuildFacts(7, 8, ModeArithmetic)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"mul:7:8":  "56",
		"mul:8:7":  "56",
		"div:56:7": "8",
		"div:56:8": "7",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}
}

func TestMissesAndSlowAnswersIncreaseWeight(t *testing.T) {
	trainer, err := NewTrainer(2, 3, ModeArithmetic, filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	base := trainer.Weight("mul:2:3")
	trainer.Record("mul:2:3", "99", time.Second)
	afterMiss := trainer.Weight("mul:2:3")
	trainer.Record("mul:2:3", "6", 4*time.Second)
	afterSlow := trainer.Weight("mul:2:3")

	if afterMiss <= base {
		t.Fatalf("weight after miss = %d, want > %d", afterMiss, base)
	}
	if afterSlow <= afterMiss {
		t.Fatalf("weight after slow = %d, want > %d", afterSlow, afterMiss)
	}
}

func TestMasteredFactDropsWeight(t *testing.T) {
	trainer, err := NewTrainer(2, 3, ModeArithmetic, filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		trainer.Record("mul:2:3", "6", time.Second)
	}
	if got := trainer.Weight("mul:2:3"); got != 1 {
		t.Fatalf("mastered weight = %d, want 1", got)
	}
}

func TestNextFactAvoidsRecentFacts(t *testing.T) {
	trainer := &Trainer{
		Facts: []Fact{
			{ID: "a", Prompt: "a"},
			{ID: "b", Prompt: "b"},
			{ID: "c", Prompt: "c"},
			{ID: "d", Prompt: "d"},
		},
		Progress: Progress{Facts: map[string]*FactStats{
			"a": {Seen: 1, Misses: 20, Wrong: 20},
		}},
		Rand:          rand.New(rand.NewSource(1)),
		RecentFactIDs: []string{"a", "b", "c"},
	}

	fact := trainer.NextFact()
	if fact.ID != "d" {
		t.Fatalf("NextFact picked %s, want only non-recent fact d", fact.ID)
	}
	wantRecent := []string{"b", "c", "d"}
	if strings.Join(trainer.RecentFactIDs, ",") != strings.Join(wantRecent, ",") {
		t.Fatalf("recent facts = %#v, want %#v", trainer.RecentFactIDs, wantRecent)
	}
}

func TestNextFactAvoidsImmediateRepeatInSmallDeck(t *testing.T) {
	trainer := &Trainer{
		Facts: []Fact{
			{ID: "a", Prompt: "a"},
			{ID: "b", Prompt: "b"},
		},
		Progress: Progress{Facts: map[string]*FactStats{
			"a": {Seen: 1, Misses: 50, Wrong: 50},
		}},
		Rand:          rand.New(rand.NewSource(2)),
		RecentFactIDs: []string{"a"},
	}

	fact := trainer.NextFact()
	if fact.ID != "b" {
		t.Fatalf("NextFact picked %s, want non-recent fact b", fact.ID)
	}
	if strings.Join(trainer.RecentFactIDs, ",") != "b" {
		t.Fatalf("recent facts = %#v, want only b", trainer.RecentFactIDs)
	}
}

func TestRelationshipModeAliases(t *testing.T) {
	cases := map[string]Mode{
		"relationships":            ModeRelationships,
		"relations":                ModeRelationships,
		"math-relations":           ModeRelationships,
		"fraction-relations":       ModeFractions,
		"percentage-relations":     ModePercentRelations,
		"log-relations":            ModeExponentsLogs,
		"root-exponent-log":        ModeExponentsLogs,
		"roots-exponents-logs":     ModeExponentsLogs,
		"algebra-identities":       ModeAlgebraIdentities,
		"identities":               ModeAlgebraIdentities,
		"algebra-patterns":         ModeAlgebraIdentities,
		"triangles":                ModeTriangles,
		"geometry-triangles":       ModeTriangles,
		"trig-triangles":           ModeTriangles,
		"relationship-trainer":     ModeRelationships,
		"percentage-relationships": ModePercentRelations,
	}
	for input, want := range cases {
		if got := parseMode(input); got != want {
			t.Fatalf("parseMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInteractiveShellModeChoices(t *testing.T) {
	cases := map[string]Mode{
		"":                     ModeRelationships,
		"1":                    ModeArithmetic,
		"5":                    ModeUnitCircle,
		"unit circle":          ModeUnitCircle,
		"root-exponent-log":    ModeExponentsLogs,
		"algebra identities":   ModeAlgebraIdentities,
		"geometry-triangles":   ModeTriangles,
		"relationship-trainer": ModeRelationships,
	}
	for input, want := range cases {
		got, ok := parseShellModeChoice(input)
		if !ok {
			t.Fatalf("parseShellModeChoice(%q) was not recognized", input)
		}
		if got != want {
			t.Fatalf("parseShellModeChoice(%q) = %q, want %q", input, got, want)
		}
	}
	if _, ok := parseShellModeChoice("999"); ok {
		t.Fatal("out-of-range numeric mode should not be recognized")
	}
	if _, ok := parseShellModeChoice("not-a-mode"); ok {
		t.Fatal("unknown mode should not be recognized")
	}
}

func TestInteractiveShellLessonChoices(t *testing.T) {
	cases := []struct {
		mode  Mode
		input string
		want  string
	}{
		{mode: ModeUnitCircle, input: "", want: string(UnitCircleLessonConcepts)},
		{mode: ModeUnitCircle, input: "2", want: string(UnitCircleLessonQuadrants)},
		{mode: ModeUnitCircle, input: "reference values", want: string(UnitCircleLessonReferenceValues)},
		{mode: ModeAlgebraIdentities, input: "", want: string(AlgebraIdentityLessonConcepts)},
		{mode: ModeAlgebraIdentities, input: "factor", want: string(AlgebraIdentityLessonFactor)},
		{mode: ModeTriangles, input: "", want: string(TriangleLessonConcepts)},
		{mode: ModeTriangles, input: "special right", want: string(TriangleLessonSpecial)},
		{mode: ModeTriangles, input: "5", want: string(TriangleLessonSOHCAHTOA)},
	}
	for _, tc := range cases {
		got, ok := parseShellLessonChoice(tc.mode, tc.input)
		if !ok {
			t.Fatalf("parseShellLessonChoice(%q, %q) was not recognized", tc.mode, tc.input)
		}
		if got != tc.want {
			t.Fatalf("parseShellLessonChoice(%q, %q) = %q, want %q", tc.mode, tc.input, got, tc.want)
		}
	}
	if _, ok := parseShellLessonChoice(ModeFractions, "anything"); ok {
		t.Fatal("non-lesson mode should not accept a lesson")
	}
	if _, ok := parseShellLessonChoice(ModeTriangles, "999"); ok {
		t.Fatal("out-of-range lesson number should not be recognized")
	}
}

func TestRelationshipsModeExcludesArithmeticFacts(t *testing.T) {
	facts := BuildFacts(2, 12, ModeRelationships)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	if _, ok := byID["mul:2:2"]; ok {
		t.Fatal("relationships mode should not include arithmetic facts")
	}
	for _, id := range []string{"frac2dec:1:2", "pctrel:20:35", "uc:concept:sin-y", "exlog:concept:log-asks-for", "algid:expand:square-sum", "tri:concept:angle-sum"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("relationships mode missing %s", id)
		}
	}
}

func TestBuildFractionFactsIncludesDecimalPairs(t *testing.T) {
	facts := BuildFacts(2, 12, ModeFractions)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"frac2dec:1:8":         ".125",
		"dec2frac:1:8":         "1/8",
		"frac2dec:3:4":         ".75",
		"dec2frac:3:4":         "3/4",
		"frac:numerator:3:4":   "3",
		"frac:denominator:3:4": "4",
		"frac:complement:3:4":  "1/4",
		"frac:reciprocal:3:4":  "4/3",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}

	fact := byID["frac:complement:3:4"]
	if fact.Explanation == "" || !hasTag(fact, "complements-to-one") {
		t.Fatalf("fraction relationship metadata = explanation %q tags %#v", fact.Explanation, fact.RelationshipTags)
	}
}

func TestFractionAnswerMatching(t *testing.T) {
	fracToDec := Fact{Kind: "fraction_to_decimal", Answer: ".125"}
	if !answerMatches(fracToDec, "0.125") {
		t.Fatal("0.125 should match .125")
	}
	if !answerMatches(fracToDec, ".1250") {
		t.Fatal(".1250 should match .125")
	}

	decToFrac := Fact{Kind: "decimal_to_fraction", Answer: "1/8"}
	if !answerMatches(decToFrac, "2/16") {
		t.Fatal("2/16 should match 1/8")
	}
	if answerMatches(decToFrac, ".125") {
		t.Fatal("decimal answer should not satisfy decimal-to-fraction prompt")
	}

	exact := Fact{Kind: "exact_quantity", Answer: "1/4"}
	for _, answer := range []string{"1/4", "2/8", ".25", "0.25"} {
		if !answerMatches(exact, answer) {
			t.Fatalf("%q should match exact quantity 1/4", answer)
		}
	}
}

func TestBuildPercentageFactsIncludesRelationshipFamilies(t *testing.T) {
	facts := BuildFacts(2, 12, ModePercentages)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"frac2pct:1:8":   "12.5%",
		"pct2frac:1:8":   "1/8",
		"dec2pct:1:8":    "12.5%",
		"pct2dec:1:8":    ".125",
		"pct:per100:1:8": "12.5",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}

	fact := byID["pct2dec:1:8"]
	if fact.Explanation == "" || !hasTag(fact, "percent-as-multiplier") {
		t.Fatalf("percentage relationship metadata = explanation %q tags %#v", fact.Explanation, fact.RelationshipTags)
	}
}

func TestPercentageAnswerMatching(t *testing.T) {
	fracToPct := Fact{Kind: "fraction_to_percent", Answer: "12.5%"}
	for _, answer := range []string{"12.5%", "12.5", ".125", "0.125", "1/8"} {
		if !answerMatches(fracToPct, answer) {
			t.Fatalf("%q should match 12.5%%", answer)
		}
	}

	pctToDec := Fact{Kind: "percent_to_decimal", Answer: ".25"}
	for _, answer := range []string{"25%", "25", ".25", "0.25", "1/4"} {
		if !answerMatches(pctToDec, answer) {
			t.Fatalf("%q should match .25", answer)
		}
	}
}

func TestBuildPercentageRelationFactsIncludesSwappedPairs(t *testing.T) {
	facts := BuildFacts(2, 12, ModePercentRelations)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"pctrel:20:35": "35% of 20",
		"pctrel:35:20": "20% of 35",
		"pctrel:4:75":  "75% of 4",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}

	fact := byID["pctrel:20:35"]
	if fact.Explanation == "" || !hasTag(fact, "commutative-multiplication") {
		t.Fatalf("percent relationship metadata = explanation %q tags %#v", fact.Explanation, fact.RelationshipTags)
	}
}

func TestPercentageRelationshipAnswerMatching(t *testing.T) {
	fact := Fact{Kind: "percent_relationship", Answer: "35% of 20", A: 20, B: 35}
	for _, answer := range []string{"35% of 20", "35%of20", "35.0% of 20.0", "7", "7.0", "14/2"} {
		if !answerMatches(fact, answer) {
			t.Fatalf("%q should match 35%% of 20", answer)
		}
	}

	for _, answer := range []string{"20% of 35", "70% of 10", "7%"} {
		if answerMatches(fact, answer) {
			t.Fatalf("%q should not match the swapped relationship", answer)
		}
	}
}

func TestBuildUnitCircleFactsIncludesCoreRelationships(t *testing.T) {
	facts := BuildFacts(2, 12, ModeUnitCircle)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"uc:deg2rad:30":                "pi/6",
		"uc:rad2deg:30":                "30 deg",
		"uc:sin:deg:30":                "1/2",
		"uc:cos:rad:150":               "-sqrt(3)/2",
		"uc:tan:deg:90":                "undefined",
		"uc:point:deg:30":              "(sqrt(3)/2,1/2)",
		"uc:point-sin:60":              "sqrt(3)/2",
		"uc:ref:150":                   "30 deg",
		"uc:ref-rad:210":               "pi/6",
		"uc:triangle:210":              "30-60-90",
		"uc:ref-value-sin:210":         "-1/2",
		"uc:quadrant:225":              "III",
		"uc:cof:sin-cos:30":            "60 deg",
		"uc:sign:cos:ii":               "-",
		"uc:concept:sin-y":             "y",
		"uc:concept:tan-ratio":         "y/x",
		"uc:reciprocal:cot:sqrt3":      "sqrt(3)/3",
		"uc:reciprocal:sec:1over2":     "2",
		"uc:reciprocal:csc:sqrt3over2": "2sqrt(3)/3",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}

	fact := byID["uc:point-sin:60"]
	if fact.Explanation == "" {
		t.Fatal("relationship fact should include an explanation")
	}
	if !hasTag(fact, "sin-is-y") || !hasTag(fact, "angle-to-coordinate") {
		t.Fatalf("point-sin tags = %#v, want sin-is-y and angle-to-coordinate", fact.RelationshipTags)
	}
}

func TestUnitCircleLessonParsingAndDefault(t *testing.T) {
	if got := lessonForMode(ModeUnitCircle, ""); got != UnitCircleLessonConcepts {
		t.Fatalf("default unit-circle lesson = %q, want %q", got, UnitCircleLessonConcepts)
	}
	if got := lessonForMode(ModeFractions, "quadrants"); got != UnitCircleLessonMixed {
		t.Fatalf("non-unit-circle lesson = %q, want ignored mixed lesson", got)
	}
	if got := parseUnitCircleLesson("reference-values"); got != UnitCircleLessonReferenceValues {
		t.Fatalf("parse reference-values = %q", got)
	}
}

func TestAlgebraIdentityLessonParsingAndDefault(t *testing.T) {
	if got := algebraIdentityLessonForMode(ModeAlgebraIdentities, ""); got != AlgebraIdentityLessonConcepts {
		t.Fatalf("default algebra-identities lesson = %q, want %q", got, AlgebraIdentityLessonConcepts)
	}
	if got := algebraIdentityLessonForMode(ModeFractions, "expand"); got != AlgebraIdentityLessonMixed {
		t.Fatalf("non-algebra lesson = %q, want ignored mixed lesson", got)
	}
	if got := parseAlgebraIdentityLesson("recognize"); got != AlgebraIdentityLessonRecognize {
		t.Fatalf("parse recognize = %q", got)
	}
	lessons := lessonsForMode(ModeAlgebraIdentities, "factor")
	if lessons.UnitCircle != UnitCircleLessonMixed {
		t.Fatalf("algebra mode unit-circle lesson = %q, want mixed", lessons.UnitCircle)
	}
	if lessons.AlgebraIdentities != AlgebraIdentityLessonFactor {
		t.Fatalf("algebra mode lesson = %q, want factor", lessons.AlgebraIdentities)
	}
}

func TestTriangleLessonParsingAndDefault(t *testing.T) {
	if got := triangleLessonForMode(ModeTriangles, ""); got != TriangleLessonConcepts {
		t.Fatalf("default triangles lesson = %q, want %q", got, TriangleLessonConcepts)
	}
	if got := triangleLessonForMode(ModeFractions, "pythagorean"); got != TriangleLessonMixed {
		t.Fatalf("non-triangles lesson = %q, want ignored mixed lesson", got)
	}
	if got := parseTriangleLesson("special-right"); got != TriangleLessonSpecial {
		t.Fatalf("parse special-right = %q", got)
	}
	lessons := lessonsForMode(ModeTriangles, "sohcahtoa")
	if lessons.UnitCircle != UnitCircleLessonMixed {
		t.Fatalf("triangles mode unit-circle lesson = %q, want mixed", lessons.UnitCircle)
	}
	if lessons.AlgebraIdentities != AlgebraIdentityLessonMixed {
		t.Fatalf("triangles mode algebra lesson = %q, want mixed", lessons.AlgebraIdentities)
	}
	if lessons.Triangles != TriangleLessonSOHCAHTOA {
		t.Fatalf("triangles mode lesson = %q, want sohcahtoa", lessons.Triangles)
	}
}

func TestBuildFactsWithUnitCircleDefaultLessonUsesConcepts(t *testing.T) {
	facts := BuildFactsWithLesson(2, 12, ModeUnitCircle, UnitCircleLessonConcepts)
	if len(facts) == 0 {
		t.Fatal("concepts lesson should return facts")
	}
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}
	if _, ok := byID["uc:concept:sin-y"]; !ok {
		t.Fatal("concepts lesson should include sine-is-y concept")
	}
	for _, forbidden := range []string{"uc:sin:deg:120", "uc:cos:rad:330", "uc:tan:rad:45", "uc:concept:cot-recip", "uc:concept:sec-recip", "uc:concept:csc-recip"} {
		if _, ok := byID[forbidden]; ok {
			t.Fatalf("concepts lesson should not include %s", forbidden)
		}
	}
	for _, fact := range facts {
		if fact.Kind == "unit_circle_value" || strings.HasPrefix(fact.Prompt, "sin(") || strings.HasPrefix(fact.Prompt, "cos(") || strings.HasPrefix(fact.Prompt, "tan(") {
			t.Fatalf("concepts lesson contains angle value prompt: %#v", fact)
		}
	}
}

func TestBuildFactsWithAlgebraIdentitiesDefaultLessonUsesConcepts(t *testing.T) {
	facts := BuildFactsWithLessons(2, 12, ModeAlgebraIdentities, lessonsForMode(ModeAlgebraIdentities, ""))
	if len(facts) == 0 {
		t.Fatal("concepts lesson should return facts")
	}
	byID := factsByID(facts)
	if _, ok := byID["algid:concept:identity"]; !ok {
		t.Fatal("concepts lesson should include identity concept")
	}
	for _, forbidden := range []string{"algid:expand:square-sum", "algid:factor:square-sum", "algid:recognize:x2-minus-9"} {
		if _, ok := byID[forbidden]; ok {
			t.Fatalf("concepts lesson should not include %s", forbidden)
		}
	}
	for _, fact := range facts {
		if fact.Kind == "algebra_identity" || strings.HasPrefix(fact.Prompt, "expand ") || strings.HasPrefix(fact.Prompt, "factor ") {
			t.Fatalf("concepts lesson contains transformation prompt: %#v", fact)
		}
	}
}

func TestBuildFactsWithTrianglesDefaultLessonUsesConcepts(t *testing.T) {
	facts := BuildFactsWithLessons(2, 12, ModeTriangles, lessonsForMode(ModeTriangles, ""))
	if len(facts) == 0 {
		t.Fatal("concepts lesson should return facts")
	}
	byID := factsByID(facts)
	for _, id := range []string{"tri:concept:right-angle", "tri:side:hypotenuse", "tri:side:opposite", "tri:side:adjacent"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("concepts lesson missing %s", id)
		}
	}
	for _, forbidden := range []string{"tri:concept:angle-sum", "tri:angle-sum:missing:60:60", "tri:pythagorean:formula", "tri:special:30-60-90:ratio", "tri:trig:sin-ratio"} {
		if _, ok := byID[forbidden]; ok {
			t.Fatalf("concepts lesson should not include %s", forbidden)
		}
	}
	for _, fact := range facts {
		if fact.Operator == "angle-sum" || fact.Operator == "pythagorean" || fact.Operator == "sin" || fact.Operator == "cos" || fact.Operator == "tan" {
			t.Fatalf("concepts lesson contains later relationship prompt: %#v", fact)
		}
	}
}

func TestBuildUnitCircleLessonFactsFiltersByStage(t *testing.T) {
	cases := []struct {
		lesson UnitCircleLesson
		wantID string
		check  func(*testing.T, []Fact)
	}{
		{
			lesson: UnitCircleLessonConcepts,
			wantID: "uc:concept:point-order",
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 4 {
					t.Fatalf("concepts returned %d facts, want 4", len(facts))
				}
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "uc:concept:") {
						t.Fatalf("concepts contains non-concept fact %s", fact.ID)
					}
					if fact.Kind == "unit_circle_value" {
						t.Fatalf("concepts contains value fact %s", fact.ID)
					}
					if hasTag(fact, "reciprocal-functions") {
						t.Fatalf("concepts contains reciprocal fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonQuadrants,
			wantID: "uc:sign:sin:iv",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if fact.Kind != "unit_circle_quadrant" && fact.Kind != "unit_circle_sign" {
						t.Fatalf("quadrants contains %s kind %s", fact.ID, fact.Kind)
					}
					if strings.HasPrefix(fact.ID, "uc:ref-value:") {
						t.Fatalf("quadrants should not contain exact trig values: %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonReferenceAngles,
			wantID: "uc:ref-rad:330",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if fact.Kind == "unit_circle_value" {
						t.Fatalf("reference-angles contains value fact %s", fact.ID)
					}
					if !(strings.HasPrefix(fact.ID, "uc:ref:") || strings.HasPrefix(fact.ID, "uc:ref-rad:") || strings.HasPrefix(fact.ID, "uc:quadrant:")) {
						t.Fatalf("reference-angles contains unrelated fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonReferenceValues,
			wantID: "uc:tan:rad:45",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if fact.Kind != "unit_circle_value" {
						t.Fatalf("reference-values contains non-value fact %s", fact.ID)
					}
					if !endsWithAny(fact.ID, []string{":30", ":45", ":60"}) {
						t.Fatalf("reference-values contains non-reference angle fact %s", fact.ID)
					}
					if strings.HasPrefix(fact.Answer, "-") {
						t.Fatalf("reference-values contains negative answer %s = %s", fact.ID, fact.Answer)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonRadians,
			wantID: "uc:deg2rad:330",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "uc:deg2rad:") && !strings.HasPrefix(fact.ID, "uc:rad2deg:") {
						t.Fatalf("radians contains non-conversion fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonAssemble,
			wantID: "uc:assemble:sin:330",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "uc:assemble:") {
						t.Fatalf("assemble contains unrelated fact %s", fact.ID)
					}
					if fact.Operator == "tan" {
						t.Fatalf("assemble should not contain tangent fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonTangent,
			wantID: "uc:tan:deg:90",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if fact.Operator != "tan" {
						t.Fatalf("tangent contains non-tangent fact %s operator %s", fact.ID, fact.Operator)
					}
					if strings.HasPrefix(fact.ID, "uc:ref-value-sin:") || strings.HasPrefix(fact.ID, "uc:ref-value-cos:") {
						t.Fatalf("tangent contains sin/cos assemble fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonReciprocals,
			wantID: "uc:reciprocal-angle:sec:deg:60",
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if !hasTag(fact, "reciprocal-functions") {
						t.Fatalf("reciprocals contains non-reciprocal fact %s tags %#v", fact.ID, fact.RelationshipTags)
					}
				}
			},
		},
		{
			lesson: UnitCircleLessonMixed,
			wantID: "uc:sin:deg:120",
			check: func(t *testing.T, facts []Fact) {
				byID := factsByID(facts)
				for _, id := range []string{"uc:concept:sin-y", "uc:quadrant:225", "uc:ref-value-sin:210", "uc:reciprocal:cot:sqrt3"} {
					if _, ok := byID[id]; !ok {
						t.Fatalf("mixed lesson missing %s", id)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.lesson), func(t *testing.T) {
			facts := BuildUnitCircleLessonFacts(tc.lesson)
			if len(facts) == 0 {
				t.Fatal("lesson returned no facts")
			}
			if _, ok := factsByID(facts)[tc.wantID]; !ok {
				t.Fatalf("lesson missing expected fact %s", tc.wantID)
			}
			tc.check(t, facts)
		})
	}
}

func TestBuildAlgebraIdentityLessonFactsFiltersByStage(t *testing.T) {
	cases := []struct {
		lesson  AlgebraIdentityLesson
		wantIDs []string
		check   func(*testing.T, []Fact)
	}{
		{
			lesson: AlgebraIdentityLessonConcepts,
			wantIDs: []string{
				"algid:concept:identity",
				"algid:concept:expand",
				"algid:concept:factor",
				"algid:concept:equivalent-expression",
				"algid:concept:distribute",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 5 {
					t.Fatalf("concepts returned %d facts, want 5", len(facts))
				}
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "algid:concept:") {
						t.Fatalf("concepts contains non-concept fact %s", fact.ID)
					}
					if fact.Kind != "algebra_identity_concept" {
						t.Fatalf("concepts contains kind %s for %s", fact.Kind, fact.ID)
					}
					if strings.HasPrefix(fact.Prompt, "expand ") || strings.HasPrefix(fact.Prompt, "factor ") {
						t.Fatalf("concepts contains transformation prompt %q", fact.Prompt)
					}
				}
			},
		},
		{
			lesson: AlgebraIdentityLessonExpand,
			wantIDs: []string{
				"algid:expand:square-sum",
				"algid:expand:square-difference",
				"algid:expand:difference-squares",
				"algid:expand:x-times-sum",
				"algid:expand:distributive",
				"algid:expand:distributive-difference",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 6 {
					t.Fatalf("expand returned %d facts, want 6", len(facts))
				}
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "algid:expand:") {
						t.Fatalf("expand contains non-expand fact %s", fact.ID)
					}
					if !strings.HasPrefix(fact.Prompt, "expand ") {
						t.Fatalf("expand contains non-forward prompt %q", fact.Prompt)
					}
					if strings.Contains(fact.Answer, "(") {
						t.Fatalf("expand answer should be expanded expression, got %s = %s", fact.ID, fact.Answer)
					}
				}
			},
		},
		{
			lesson: AlgebraIdentityLessonFactor,
			wantIDs: []string{
				"algid:factor:square-sum",
				"algid:factor:square-difference",
				"algid:factor:difference-squares",
				"algid:factor:x-common-factor",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 4 {
					t.Fatalf("factor returned %d facts, want 4", len(facts))
				}
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "algid:factor:") {
						t.Fatalf("factor contains non-factor fact %s", fact.ID)
					}
					if !strings.HasPrefix(fact.Prompt, "factor ") {
						t.Fatalf("factor contains non-reverse prompt %q", fact.Prompt)
					}
				}
			},
		},
		{
			lesson: AlgebraIdentityLessonRecognize,
			wantIDs: []string{
				"algid:recognize:x2-minus-9",
				"algid:recognize:x2-plus-6x-plus-9",
				"algid:recognize:4x2-minus-25",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 3 {
					t.Fatalf("recognize returned %d facts, want 3", len(facts))
				}
				for _, fact := range facts {
					if !strings.HasPrefix(fact.ID, "algid:recognize:") {
						t.Fatalf("recognize contains non-recognition fact %s", fact.ID)
					}
					if fact.Kind != "algebra_pattern_name" {
						t.Fatalf("recognize fact %s kind = %q, want algebra_pattern_name", fact.ID, fact.Kind)
					}
					if !strings.HasPrefix(fact.Prompt, "what pattern") {
						t.Fatalf("recognize prompt should ask for a pattern name, got %q", fact.Prompt)
					}
					if strings.ContainsAny(fact.Answer, "^()+=") {
						t.Fatalf("recognize answer should be a pattern name, got %q", fact.Answer)
					}
				}
			},
		},
		{
			lesson: AlgebraIdentityLessonMixed,
			wantIDs: []string{
				"algid:concept:identity",
				"algid:expand:x-times-sum",
				"algid:factor:x-common-factor",
				"algid:recognize:x2-minus-9",
				"algid:factor:sum-cubes",
				"algid:factor:numeric-difference-squares-9",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) <= 12 {
					t.Fatalf("mixed returned %d facts, want the full algebra identity deck", len(facts))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.lesson), func(t *testing.T) {
			facts := BuildAlgebraIdentityLessonFacts(tc.lesson)
			if len(facts) == 0 {
				t.Fatal("lesson returned no facts")
			}
			byID := factsByID(facts)
			for _, id := range tc.wantIDs {
				if _, ok := byID[id]; !ok {
					t.Fatalf("lesson missing expected fact %s", id)
				}
			}
			tc.check(t, facts)
		})
	}
}

func TestBuildTriangleLessonFactsFiltersByStage(t *testing.T) {
	cases := []struct {
		lesson  TriangleLesson
		wantIDs []string
		check   func(*testing.T, []Fact)
	}{
		{
			lesson: TriangleLessonConcepts,
			wantIDs: []string{
				"tri:concept:right-angle",
				"tri:side:hypotenuse",
				"tri:side:opposite",
				"tri:side:adjacent",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 4 {
					t.Fatalf("concepts returned %d facts, want 4", len(facts))
				}
				allowed := map[string]bool{
					"tri:concept:right-angle": true,
					"tri:side:hypotenuse":     true,
					"tri:side:opposite":       true,
					"tri:side:adjacent":       true,
				}
				for _, fact := range facts {
					if !allowed[fact.ID] {
						t.Fatalf("concepts contains unrelated fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: TriangleLessonAngleSum,
			wantIDs: []string{
				"tri:concept:angle-sum",
				"tri:concept:acute-complement",
				"tri:angle-sum:missing:60:60",
				"tri:angle-sum:missing:30:90",
				"tri:angle-sum:missing:45:45",
			},
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if fact.Operator != "angle-sum" {
						t.Fatalf("angle-sum contains non-angle-sum fact %s operator %s", fact.ID, fact.Operator)
					}
					if strings.Contains(fact.ID, "pythagorean") {
						t.Fatalf("angle-sum contains pythagorean fact %s", fact.ID)
					}
				}
			},
		},
		{
			lesson: TriangleLessonPythagorean,
			wantIDs: []string{
				"tri:pythagorean:formula",
				"tri:pythagorean:3-4-5:hypotenuse",
				"tri:pythagorean:5-12-13:leg",
			},
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if !hasTag(fact, "pythagorean-theorem") {
						t.Fatalf("pythagorean contains non-pythagorean fact %s tags %#v", fact.ID, fact.RelationshipTags)
					}
				}
			},
		},
		{
			lesson: TriangleLessonSpecial,
			wantIDs: []string{
				"tri:special:45-45-90:ratio",
				"tri:special:30-60-90:ratio",
				"tri:special:30-60-90:opposite-30",
				"tri:special:30-60-90:opposite-60",
				"tri:special:45-45-90:hypotenuse-from-leg-5",
				"tri:special:30-60-90:hypotenuse-from-short-4",
				"tri:special:30-60-90:long-from-short-4",
			},
			check: func(t *testing.T, facts []Fact) {
				for _, fact := range facts {
					if !hasTag(fact, "special-right-triangles") {
						t.Fatalf("special-right contains unrelated fact %s tags %#v", fact.ID, fact.RelationshipTags)
					}
				}
			},
		},
		{
			lesson: TriangleLessonSOHCAHTOA,
			wantIDs: []string{
				"tri:trig:sin-ratio",
				"tri:trig:cos-ratio",
				"tri:trig:tan-ratio",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) != 3 {
					t.Fatalf("sohcahtoa returned %d facts, want 3", len(facts))
				}
				for _, fact := range facts {
					if !hasTag(fact, "sohcahtoa") {
						t.Fatalf("sohcahtoa contains unrelated fact %s tags %#v", fact.ID, fact.RelationshipTags)
					}
				}
			},
		},
		{
			lesson: TriangleLessonMixed,
			wantIDs: []string{
				"tri:concept:right-angle",
				"tri:angle-sum:missing:60:60",
				"tri:pythagorean:formula",
				"tri:special:30-60-90:long-from-short-4",
				"tri:trig:sin-ratio",
				"tri:similar:side-ratios",
			},
			check: func(t *testing.T, facts []Fact) {
				if len(facts) <= 15 {
					t.Fatalf("mixed returned %d facts, want the full triangle deck", len(facts))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.lesson), func(t *testing.T) {
			facts := BuildTriangleLessonFacts(tc.lesson)
			if len(facts) == 0 {
				t.Fatal("lesson returned no facts")
			}
			byID := factsByID(facts)
			for _, id := range tc.wantIDs {
				if _, ok := byID[id]; !ok {
					t.Fatalf("lesson missing expected fact %s", id)
				}
			}
			tc.check(t, facts)
		})
	}
}

func TestUnitCircleAnswerMatching(t *testing.T) {
	radian := Fact{Kind: "unit_circle_radian", Answer: "5pi/6"}
	for _, answer := range []string{"5pi/6", "5*pi/6", "5 pi / 6", "5pi/6 rad"} {
		if !answerMatches(radian, answer) {
			t.Fatalf("%q should match 5pi/6", answer)
		}
	}
	if answerMatches(radian, "150") {
		t.Fatal("degrees should not satisfy a radians prompt")
	}

	degree := Fact{Kind: "unit_circle_degree", Answer: "150 deg"}
	for _, answer := range []string{"150", "150 deg", "150 degrees"} {
		if !answerMatches(degree, answer) {
			t.Fatalf("%q should match 150 deg", answer)
		}
	}

	value := Fact{Kind: "unit_circle_value", Answer: "sqrt(3)/2"}
	for _, answer := range []string{"sqrt(3)/2", "sqrt3/2", "+sqrt(3) / 2"} {
		if !answerMatches(value, answer) {
			t.Fatalf("%q should match sqrt(3)/2", answer)
		}
	}
	if answerMatches(value, ".866") {
		t.Fatal("decimal approximation should not satisfy an exact radical prompt")
	}

	half := Fact{Kind: "unit_circle_value", Answer: "1/2"}
	for _, answer := range []string{"1/2", ".5", "0.5"} {
		if !answerMatches(half, answer) {
			t.Fatalf("%q should match 1/2", answer)
		}
	}

	undefined := Fact{Kind: "unit_circle_value", Answer: "undefined"}
	for _, answer := range []string{"undefined", "undef", "dne"} {
		if !answerMatches(undefined, answer) {
			t.Fatalf("%q should match undefined", answer)
		}
	}

	point := Fact{Kind: "unit_circle_point", Answer: "(sqrt(3)/2,1/2)"}
	for _, answer := range []string{"(sqrt(3)/2, 1/2)", "sqrt3/2,.5"} {
		if !answerMatches(point, answer) {
			t.Fatalf("%q should match the point", answer)
		}
	}

	quadrant := Fact{Kind: "unit_circle_quadrant", Answer: "III"}
	for _, answer := range []string{"III", "3", "QIII", "quadrant 3"} {
		if !answerMatches(quadrant, answer) {
			t.Fatalf("%q should match quadrant III", answer)
		}
	}

	sign := Fact{Kind: "unit_circle_sign", Answer: "-"}
	for _, answer := range []string{"-", "negative", "minus"} {
		if !answerMatches(sign, answer) {
			t.Fatalf("%q should match negative sign", answer)
		}
	}

	axis := Fact{Kind: "unit_circle_axis", Answer: "y"}
	for _, answer := range []string{"y", "y-coordinate", "y axis"} {
		if !answerMatches(axis, answer) {
			t.Fatalf("%q should match y-coordinate", answer)
		}
	}

	ratio := Fact{Kind: "unit_circle_ratio", Answer: "y/x"}
	for _, answer := range []string{"y/x", "sin/cos", "sine/cosine"} {
		if !answerMatches(ratio, answer) {
			t.Fatalf("%q should match y/x", answer)
		}
	}

	function := Fact{Kind: "unit_circle_function", Answer: "tan"}
	for _, answer := range []string{"tan", "tangent", "tan(theta)"} {
		if !answerMatches(function, answer) {
			t.Fatalf("%q should match tangent", answer)
		}
	}

	pointOrder := Fact{Kind: "unit_circle_point_order", Answer: "(cos,sin)"}
	for _, answer := range []string{"(cos(theta), sin(theta))", "cos,sin", "x,y"} {
		if !answerMatches(pointOrder, answer) {
			t.Fatalf("%q should match point order", answer)
		}
	}

	triangle := Fact{Kind: "unit_circle_triangle", Answer: "30-60-90"}
	for _, answer := range []string{"30-60-90", "30/60/90", "30,60,90 triangle"} {
		if !answerMatches(triangle, answer) {
			t.Fatalf("%q should match 30-60-90", answer)
		}
	}

	reciprocal := Fact{Kind: "unit_circle_value", Answer: "sqrt(3)/3"}
	for _, answer := range []string{"sqrt(3)/3", "sqrt3/3", "1/sqrt3"} {
		if !answerMatches(reciprocal, answer) {
			t.Fatalf("%q should match sqrt(3)/3", answer)
		}
	}
}

func hasTag(fact Fact, tag string) bool {
	for _, got := range fact.RelationshipTags {
		if got == tag {
			return true
		}
	}
	return false
}

func factsByID(facts []Fact) map[string]Fact {
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}
	return byID
}

func TestBuildExponentLogFactsIncludesCoreRelationships(t *testing.T) {
	facts := BuildFacts(2, 12, ModeExponentsLogs)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"exlog:pow:2:5":               "32",
		"exlog:log:2:5":               "5",
		"exlog:pow:2:-3":              "1/8",
		"exlog:log:2:-3":              "-3",
		"exlog:pow2log:2:5":           "log_2(32)=5",
		"exlog:log2pow:2:5":           "2^5=32",
		"exlog:pow2root:2:5":          "root_5(32)=2",
		"exlog:log2root:2:5":          "root_5(32)=2",
		"exlog:root2pow:2:5":          "2^5=32",
		"exlog:root2log:2:5":          "log_2(32)=5",
		"exlog:missing-exp:2:5":       "5",
		"exlog:missing-value:2:5":     "32",
		"exlog:missing-base-log:2:5":  "2",
		"exlog:inverse-log-pow:2:5":   "5",
		"exlog:inverse-pow-log:2:5":   "32",
		"exlog:concept:log-asks-for":  "exponent",
		"exlog:concept:zero-exponent": "1",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
	}

	fact := byID["exlog:log:2:5"]
	if fact.Explanation == "" || !hasTag(fact, "exponential-logarithmic-equivalence") {
		t.Fatalf("exponent/log metadata = explanation %q tags %#v", fact.Explanation, fact.RelationshipTags)
	}

	for _, id := range []string{"exlog:pow2log:2:5", "exlog:pow2root:2:5", "exlog:log2pow:2:5", "exlog:log2root:2:5", "exlog:root2pow:2:5", "exlog:root2log:2:5"} {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing directed relationship fact %s", id)
		}
		if fact.Explanation == "" || len(fact.RelationshipTags) == 0 {
			t.Fatalf("%s missing relationship metadata: explanation %q tags %#v", id, fact.Explanation, fact.RelationshipTags)
		}
	}
	if byID["exlog:pow2root:2:5"].Prompt == byID["exlog:pow2log:2:5"].Prompt {
		t.Fatal("exponent-to-root and exponent-to-log should have distinct prompts for independent tracking")
	}
	if !hasTag(byID["exlog:pow2root:2:5"], "target-root") || !hasTag(byID["exlog:root2log:2:5"], "source-root") {
		t.Fatalf("root conversion tags missing: pow2root %#v root2log %#v", byID["exlog:pow2root:2:5"].RelationshipTags, byID["exlog:root2log:2:5"].RelationshipTags)
	}
}

func TestExponentLogAnswerMatching(t *testing.T) {
	number := Fact{Kind: "exponent_log_number", Answer: "1/8"}
	for _, answer := range []string{"1/8", "2/16", ".125", "0.125"} {
		if !answerMatches(number, answer) {
			t.Fatalf("%q should match 1/8", answer)
		}
	}
	if answerMatches(number, "12.5%") {
		t.Fatal("percent answer should not satisfy an exponent/log quantity")
	}

	logForm := Fact{Kind: "exponent_log_form", Answer: "log_2(32)=5"}
	for _, answer := range []string{"log_2(32)=5", "log2(32)=5", "log_2(32) = 5"} {
		if !answerMatches(logForm, answer) {
			t.Fatalf("%q should match log_2(32)=5", answer)
		}
	}
	if answerMatches(logForm, "2^5=32") {
		t.Fatal("exponent form should not satisfy a log-form prompt")
	}

	exponentForm := Fact{Kind: "log_exponent_form", Answer: "2^-3=1/8"}
	for _, answer := range []string{"2^-3=1/8", "2^(-3)=.125", "2**-3=0.125"} {
		if !answerMatches(exponentForm, answer) {
			t.Fatalf("%q should match 2^-3=1/8", answer)
		}
	}
	if answerMatches(exponentForm, "log_2(1/8)=-3") {
		t.Fatal("log form should not satisfy an exponent-form prompt")
	}

	rootForm := Fact{Kind: "root_form", Answer: "root_5(32)=2"}
	for _, answer := range []string{"root_5(32)=2", "root5(32)=2", "5throot(32)=2"} {
		if !answerMatches(rootForm, answer) {
			t.Fatalf("%q should match root_5(32)=2", answer)
		}
	}
	if answerMatches(rootForm, "2^5=32") {
		t.Fatal("exponent form should not satisfy a root-form prompt")
	}
	if answerMatches(rootForm, "log_2(32)=5") {
		t.Fatal("log form should not satisfy a root-form prompt")
	}

	squareRootForm := Fact{Kind: "root_form", Answer: "root_2(16)=4"}
	for _, answer := range []string{"root_2(16)=4", "sqrt(16)=4", "squareroot(16)=4"} {
		if !answerMatches(squareRootForm, answer) {
			t.Fatalf("%q should match root_2(16)=4", answer)
		}
	}

	role := Fact{Kind: "exponent_log_role", Answer: "exponent"}
	for _, answer := range []string{"exponent", "power"} {
		if !answerMatches(role, answer) {
			t.Fatalf("%q should match exponent role", answer)
		}
	}
}

func TestBuildTriangleFactsIncludesCoreRelationships(t *testing.T) {
	facts := BuildFacts(2, 12, ModeTriangles)
	byID := factsByID(facts)

	cases := map[string]string{
		"tri:concept:angle-sum":                        "180",
		"tri:concept:right-angle":                      "90",
		"tri:concept:acute-complement":                 "90",
		"tri:angle-sum:missing:60:60":                  "60",
		"tri:angle-sum:missing:30:90":                  "60",
		"tri:angle-sum:missing:45:45":                  "90",
		"tri:side:hypotenuse":                          "hypotenuse",
		"tri:side:opposite":                            "opposite",
		"tri:side:adjacent":                            "adjacent",
		"tri:pythagorean:formula":                      "a^2+b^2=c^2",
		"tri:pythagorean:3-4-5:hypotenuse":             "5",
		"tri:pythagorean:5-12-13:leg":                  "12",
		"tri:special:45-45-90:ratio":                   "1:1:sqrt(2)",
		"tri:special:30-60-90:ratio":                   "1:sqrt(3):2",
		"tri:special:30-60-90:opposite-30":             "short leg",
		"tri:special:30-60-90:opposite-60":             "long leg",
		"tri:special:45-45-90:hypotenuse-from-leg-5":   "5sqrt(2)",
		"tri:special:30-60-90:hypotenuse-from-short-4": "8",
		"tri:special:30-60-90:long-from-short-4":       "4sqrt(3)",
		"tri:trig:sin-ratio":                           "opposite/hypotenuse",
		"tri:trig:cos-ratio":                           "adjacent/hypotenuse",
		"tri:trig:tan-ratio":                           "opposite/adjacent",
		"tri:similar:side-ratios":                      "proportional",
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
		if fact.Explanation == "" || len(fact.RelationshipTags) == 0 {
			t.Fatalf("%s missing relationship metadata: explanation %q tags %#v", id, fact.Explanation, fact.RelationshipTags)
		}
	}

	if !hasTag(byID["tri:trig:sin-ratio"], "trig-foundations") {
		t.Fatalf("sin ratio tags = %#v, want trig-foundations", byID["tri:trig:sin-ratio"].RelationshipTags)
	}
	if !hasTag(byID["tri:special:30-60-90:ratio"], "special-right-triangles") {
		t.Fatalf("30-60-90 tags = %#v, want special-right-triangles", byID["tri:special:30-60-90:ratio"].RelationshipTags)
	}
}

func TestTriangleFactsIncludedOnlyInExpectedModes(t *testing.T) {
	triangleID := "tri:concept:angle-sum"
	for _, mode := range []Mode{ModeArithmetic, ModeFractions, ModePercentages, ModePercentRelations, ModeExponentsLogs, ModeAlgebraIdentities, ModeUnitCircle} {
		if _, ok := factsByID(BuildFacts(2, 12, mode))[triangleID]; ok {
			t.Fatalf("%s should not include triangle facts", mode)
		}
	}
	for _, mode := range []Mode{ModeTriangles, ModeRelationships, ModeMixed} {
		if _, ok := factsByID(BuildFacts(2, 12, mode))[triangleID]; !ok {
			t.Fatalf("%s should include triangle facts", mode)
		}
	}

	unitCircleConcepts := BuildFactsWithLesson(2, 12, ModeUnitCircle, UnitCircleLessonConcepts)
	if _, ok := factsByID(unitCircleConcepts)[triangleID]; ok {
		t.Fatal("unit-circle lesson facts should not include triangle facts")
	}
}

func TestTriangleFactIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, fact := range BuildTriangleFacts() {
		if seen[fact.ID] {
			t.Fatalf("duplicate triangle fact ID %s", fact.ID)
		}
		seen[fact.ID] = true
	}
}

func TestTriangleAnswerMatching(t *testing.T) {
	formula := Fact{Kind: "triangle_formula", Answer: "a^2+b^2=c^2"}
	for _, answer := range []string{"a^2+b^2=c^2", "a**2 + b**2 = c**2"} {
		if !answerMatches(formula, answer) {
			t.Fatalf("%q should match Pythagorean formula", answer)
		}
	}

	ratio := Fact{Kind: "triangle_ratio", Answer: "opposite/hypotenuse"}
	for _, answer := range []string{"opposite/hypotenuse", "opp/hyp", "opposite over hypotenuse"} {
		if !answerMatches(ratio, answer) {
			t.Fatalf("%q should match opposite/hypotenuse", answer)
		}
	}

	length := Fact{Kind: "triangle_length", Answer: "5sqrt(2)"}
	for _, answer := range []string{"5sqrt(2)", "5sqrt2", "5*sqrt(2)"} {
		if !answerMatches(length, answer) {
			t.Fatalf("%q should match 5sqrt(2)", answer)
		}
	}

	term := Fact{Kind: "triangle_term", Answer: "short leg"}
	for _, answer := range []string{"short leg", "short side", "short"} {
		if !answerMatches(term, answer) {
			t.Fatalf("%q should match short leg", answer)
		}
	}
}

func TestBuildAlgebraIdentityFactsIncludesCommonPatterns(t *testing.T) {
	facts := BuildFacts(2, 12, ModeAlgebraIdentities)
	byID := factsByID(facts)

	vocabulary := map[string]string{
		"algid:concept:identity":              "true for all allowed values",
		"algid:concept:equivalent-expression": "same value for the same inputs",
		"algid:concept:expand":                "write as a sum of terms",
		"algid:concept:factor":                "write as a product of factors",
		"algid:concept:distribute":            "multiply into each term",
	}
	for id, answer := range vocabulary {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %q, want %q", id, fact.Answer, answer)
		}
		if fact.Kind != "algebra_identity_concept" {
			t.Fatalf("%s kind = %q, want algebra_identity_concept", id, fact.Kind)
		}
		if fact.Explanation == "" || !hasTag(fact, "algebra-identity") {
			t.Fatalf("%s missing relationship metadata: explanation %q tags %#v", id, fact.Explanation, fact.RelationshipTags)
		}
	}

	cases := []struct {
		id   string
		want string
		tags []string
	}{
		{id: "algid:expand:distributive", want: "ab+ac", tags: []string{"algebra-identity", "distributive-property", "expand"}},
		{id: "algid:expand:distributive-difference", want: "ab-ac", tags: []string{"algebra-identity", "distributive-property", "expand"}},
		{id: "algid:expand:difference-squares", want: "a^2-b^2", tags: []string{"algebra-identity", "difference-of-squares", "expand"}},
		{id: "algid:factor:difference-squares", want: "(a+b)(a-b)", tags: []string{"algebra-identity", "difference-of-squares", "factor"}},
		{id: "algid:expand:square-sum", want: "a^2+2ab+b^2", tags: []string{"algebra-identity", "perfect-square-trinomial", "expand"}},
		{id: "algid:expand:square-difference", want: "a^2-2ab+b^2", tags: []string{"algebra-identity", "perfect-square-trinomial", "expand"}},
		{id: "algid:factor:numeric-difference-squares-9", want: "(x+3)(x-3)", tags: []string{"algebra-identity", "difference-of-squares", "factor"}},
		{id: "algid:factor:numeric-difference-squares-16", want: "(x+4)(x-4)", tags: []string{"algebra-identity", "difference-of-squares", "factor"}},
		{id: "algid:factor:numeric-perfect-square-plus", want: "(x+3)^2", tags: []string{"algebra-identity", "perfect-square-trinomial", "factor"}},
		{id: "algid:factor:numeric-perfect-square-minus", want: "(x-5)^2", tags: []string{"algebra-identity", "perfect-square-trinomial", "factor"}},
		{id: "algid:factor:sum-cubes", want: "(a+b)(a^2-ab+b^2)", tags: []string{"algebra-identity", "factor"}},
		{id: "algid:factor:difference-cubes", want: "(a-b)(a^2+ab+b^2)", tags: []string{"algebra-identity", "factor"}},
		{id: "algid:factor:common-factor-x", want: "x(y+z)", tags: []string{"algebra-identity", "factor"}},
		{id: "algid:expand:binomial-product", want: "x^2+(a+b)x+ab", tags: []string{"algebra-identity", "expand"}},
	}
	for _, tc := range cases {
		fact, ok := byID[tc.id]
		if !ok {
			t.Fatalf("missing fact %s", tc.id)
		}
		if fact.Answer != tc.want {
			t.Fatalf("%s answer = %q, want %q", tc.id, fact.Answer, tc.want)
		}
		if fact.Kind != "algebra_identity" {
			t.Fatalf("%s kind = %q, want algebra_identity", tc.id, fact.Kind)
		}
		if fact.Explanation == "" {
			t.Fatalf("%s missing explanation", tc.id)
		}
		for _, tag := range tc.tags {
			if !hasTag(fact, tag) {
				t.Fatalf("%s tags = %#v, want %s", tc.id, fact.RelationshipTags, tag)
			}
		}
	}
}

func TestAlgebraIdentitiesIncludedOnlyInExpectedModes(t *testing.T) {
	algebraID := "algid:expand:square-sum"
	for _, mode := range []Mode{ModeArithmetic, ModeFractions, ModePercentages, ModePercentRelations, ModeExponentsLogs, ModeUnitCircle} {
		if _, ok := factsByID(BuildFacts(2, 12, mode))[algebraID]; ok {
			t.Fatalf("%s should not include algebra identity facts", mode)
		}
	}
	for _, mode := range []Mode{ModeAlgebraIdentities, ModeRelationships, ModeMixed} {
		if _, ok := factsByID(BuildFacts(2, 12, mode))[algebraID]; !ok {
			t.Fatalf("%s should include algebra identity facts", mode)
		}
	}
}

func TestAlgebraIdentityFactIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, fact := range BuildAlgebraIdentityFacts() {
		if seen[fact.ID] {
			t.Fatalf("duplicate algebra identity fact ID %s", fact.ID)
		}
		seen[fact.ID] = true
	}
}

func TestAlgebraIdentityAnswerMatching(t *testing.T) {
	fact := Fact{Kind: "algebra_identity", Answer: "a^2+2ab+b^2"}
	for _, answer := range []string{"a^2+2ab+b^2", "a^2 + 2*a*b + b^2", "a**2+2ab+b**2"} {
		if !answerMatches(fact, answer) {
			t.Fatalf("%q should match square-of-sum expansion", answer)
		}
	}
	if answerMatches(fact, "a^2+b^2") {
		t.Fatal("incomplete expansion should not match")
	}

	concept := Fact{Kind: "algebra_identity_concept", Answer: "write as a product of factors"}
	for _, answer := range []string{"factor", "factoring", "write as a product of factors"} {
		if !answerMatches(concept, answer) {
			t.Fatalf("%q should match factor concept", answer)
		}
	}

	distribute := Fact{Kind: "algebra_identity_concept", Answer: "multiply into each term"}
	for _, answer := range []string{"distribute", "distribution", "multiply into each term"} {
		if !answerMatches(distribute, answer) {
			t.Fatalf("%q should match distribute concept", answer)
		}
	}

	pattern := Fact{Kind: "algebra_pattern_name", Answer: "difference of squares"}
	for _, answer := range []string{"difference of squares", "difference-of-squares", "diff of squares"} {
		if !answerMatches(pattern, answer) {
			t.Fatalf("%q should match difference of squares", answer)
		}
	}
	if answerMatches(pattern, "(x+3)(x-3)") {
		t.Fatal("recognize prompt should not accept final factoring transformation")
	}
}
