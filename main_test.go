package main

import (
	"path/filepath"
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

func TestRelationshipModeAliases(t *testing.T) {
	cases := map[string]Mode{
		"relationships":            ModeRelationships,
		"relations":                ModeRelationships,
		"math-relations":           ModeRelationships,
		"fraction-relations":       ModeFractions,
		"percentage-relations":     ModePercentRelations,
		"log-relations":            ModeExponentsLogs,
		"relationship-trainer":     ModeRelationships,
		"percentage-relationships": ModePercentRelations,
	}
	for input, want := range cases {
		if got := parseMode(input); got != want {
			t.Fatalf("parseMode(%q) = %q, want %q", input, got, want)
		}
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
	for _, id := range []string{"frac2dec:1:2", "pctrel:20:35", "uc:concept:sin-y", "exlog:concept:log-asks-for"} {
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

	role := Fact{Kind: "exponent_log_role", Answer: "exponent"}
	for _, answer := range []string{"exponent", "power"} {
		if !answerMatches(role, answer) {
			t.Fatalf("%q should match exponent role", answer)
		}
	}
}
