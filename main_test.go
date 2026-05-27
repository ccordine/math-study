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

func TestBuildFractionFactsIncludesDecimalPairs(t *testing.T) {
	facts := BuildFacts(2, 12, ModeFractions)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"frac2dec:1:8": ".125",
		"dec2frac:1:8": "1/8",
		"frac2dec:3:4": ".75",
		"dec2frac:3:4": "3/4",
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
}

func TestBuildPercentageFactsIncludesRelationshipFamilies(t *testing.T) {
	facts := BuildFacts(2, 12, ModePercentages)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]string{
		"frac2pct:1:8": "12.5%",
		"pct2frac:1:8": "1/8",
		"dec2pct:1:8":  "12.5%",
		"pct2dec:1:8":  ".125",
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
