package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBuildFactsIncludesFactFamilies(t *testing.T) {
	facts := BuildFacts(7, 8)
	byID := map[string]Fact{}
	for _, fact := range facts {
		byID[fact.ID] = fact
	}

	cases := map[string]int{
		"mul:7:8":  56,
		"mul:8:7":  56,
		"div:56:7": 8,
		"div:56:8": 7,
	}
	for id, answer := range cases {
		fact, ok := byID[id]
		if !ok {
			t.Fatalf("missing fact %s", id)
		}
		if fact.Answer != answer {
			t.Fatalf("%s answer = %d, want %d", id, fact.Answer, answer)
		}
	}
}

func TestMissesAndSlowAnswersIncreaseWeight(t *testing.T) {
	trainer, err := NewTrainer(2, 3, filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	base := trainer.Weight("mul:2:3")
	trainer.Record("mul:2:3", 99, time.Second)
	afterMiss := trainer.Weight("mul:2:3")
	trainer.Record("mul:2:3", 6, 4*time.Second)
	afterSlow := trainer.Weight("mul:2:3")

	if afterMiss <= base {
		t.Fatalf("weight after miss = %d, want > %d", afterMiss, base)
	}
	if afterSlow <= afterMiss {
		t.Fatalf("weight after slow = %d, want > %d", afterSlow, afterMiss)
	}
}

func TestMasteredFactDropsWeight(t *testing.T) {
	trainer, err := NewTrainer(2, 3, filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		trainer.Record("mul:2:3", 6, time.Second)
	}
	if got := trainer.Weight("mul:2:3"); got != 1 {
		t.Fatalf("mastered weight = %d, want 1", got)
	}
}
