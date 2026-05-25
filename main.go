package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const slowThreshold = 3 * time.Second

type Fact struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Answer   int    `json:"answer"`
	Family   string `json:"family"`
	Kind     string `json:"kind"`
	A        int    `json:"a"`
	B        int    `json:"b"`
	Product  int    `json:"product"`
	Operator string `json:"operator"`
}

type FactStats struct {
	Correct int       `json:"correct"`
	Wrong   int       `json:"wrong"`
	Misses  int       `json:"misses"`
	Slow    int       `json:"slow"`
	TotalMS int64     `json:"total_ms"`
	Seen    int       `json:"seen"`
	Updated time.Time `json:"updated"`
}

type Progress struct {
	Facts map[string]*FactStats `json:"facts"`
}

type Trainer struct {
	Facts    []Fact
	ByID     map[string]Fact
	Progress Progress
	Rand     *rand.Rand
	Path     string
}

type Attempt struct {
	ID     string `json:"id"`
	Answer int    `json:"answer"`
	MS     int64  `json:"ms"`
}

type AttemptResult struct {
	Correct bool      `json:"correct"`
	Slow    bool      `json:"slow"`
	Answer  int       `json:"answer"`
	Fact    Fact      `json:"fact"`
	Stats   FactStats `json:"stats"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "web" {
		webCmd(os.Args[2:])
		return
	}
	cliCmd(os.Args[1:])
}

func cliCmd(args []string) {
	fs := flag.NewFlagSet("math-study", flag.ExitOnError)
	min := fs.Int("min", 2, "smallest multiplication factor")
	max := fs.Int("max", 12, "largest multiplication factor")
	minutes := fs.Int("minutes", 10, "session length in minutes")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	trainer := mustTrainer(*min, *max, *progress)
	reader := bufio.NewReader(os.Stdin)
	deadline := time.Now().Add(time.Duration(*minutes) * time.Minute)

	fmt.Printf("Math trainer: factors %d-%d, mixed multiplication/division. Type q to quit.\n", *min, *max)
	for time.Now().Before(deadline) {
		fact := trainer.NextFact()
		start := time.Now()
		fmt.Printf("%s = ", fact.Prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "q") || strings.EqualFold(line, "quit") {
			break
		}
		answer, err := strconv.Atoi(line)
		if err != nil {
			fmt.Printf("Enter a number. Correct answer: %d\n", fact.Answer)
			_ = trainer.Record(fact.ID, -1, time.Since(start))
			continue
		}
		result := trainer.Record(fact.ID, answer, time.Since(start))
		if result.Correct && !result.Slow {
			fmt.Println("correct")
		} else if result.Correct {
			fmt.Printf("correct, but slow (%0.1fs)\n", float64(time.Since(start).Milliseconds())/1000)
		} else {
			fmt.Printf("missed: %s = %d\n", fact.Prompt, fact.Answer)
		}
	}

	if err := trainer.Save(); err != nil {
		log.Printf("save progress: %v", err)
	}
	printWeakFacts(trainer, 12)
}

func webCmd(args []string) {
	fs := flag.NewFlagSet("math-study web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	min := fs.Int("min", 2, "smallest multiplication factor")
	max := fs.Int("max", 12, "largest multiplication factor")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	trainer := mustTrainer(*min, *max, *progress)
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := page.Execute(w, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/api/next", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fact := trainer.NextFact()
		mu.Unlock()
		writeJSON(w, fact)
	})
	mux.HandleFunc("/api/attempt", func(w http.ResponseWriter, r *http.Request) {
		var attempt Attempt
		if err := json.NewDecoder(r.Body).Decode(&attempt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		result := trainer.Record(attempt.ID, attempt.Answer, time.Duration(attempt.MS)*time.Millisecond)
		if err := trainer.Save(); err != nil {
			mu.Unlock()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		mu.Unlock()
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		summary := map[string]any{
			"weak":   trainer.WeakFacts(20),
			"totals": trainer.Totals(),
		}
		mu.Unlock()
		writeJSON(w, summary)
	})

	log.Printf("serving http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func mustTrainer(min, max int, path string) *Trainer {
	if min < 1 || max < min {
		log.Fatalf("invalid range: min=%d max=%d", min, max)
	}
	trainer, err := NewTrainer(min, max, path)
	if err != nil {
		log.Fatal(err)
	}
	return trainer
}

func NewTrainer(min, max int, path string) (*Trainer, error) {
	facts := BuildFacts(min, max)
	progress, err := LoadProgress(path)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Fact, len(facts))
	for _, fact := range facts {
		byID[fact.ID] = fact
	}
	return &Trainer{
		Facts:    facts,
		ByID:     byID,
		Progress: progress,
		Rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
		Path:     path,
	}, nil
}

func BuildFacts(min, max int) []Fact {
	var facts []Fact
	for a := min; a <= max; a++ {
		for b := a; b <= max; b++ {
			product := a * b
			family := fmt.Sprintf("%dx%d=%d", a, b, product)
			facts = append(facts,
				Fact{
					ID:       fmt.Sprintf("mul:%d:%d", a, b),
					Prompt:   fmt.Sprintf("%d x %d", a, b),
					Answer:   product,
					Family:   family,
					Kind:     "multiply",
					A:        a,
					B:        b,
					Product:  product,
					Operator: "x",
				},
				Fact{
					ID:       fmt.Sprintf("mul:%d:%d", b, a),
					Prompt:   fmt.Sprintf("%d x %d", b, a),
					Answer:   product,
					Family:   family,
					Kind:     "multiply",
					A:        b,
					B:        a,
					Product:  product,
					Operator: "x",
				},
				Fact{
					ID:       fmt.Sprintf("div:%d:%d", product, a),
					Prompt:   fmt.Sprintf("%d / %d", product, a),
					Answer:   b,
					Family:   family,
					Kind:     "divide",
					A:        product,
					B:        a,
					Product:  product,
					Operator: "/",
				},
				Fact{
					ID:       fmt.Sprintf("div:%d:%d", product, b),
					Prompt:   fmt.Sprintf("%d / %d", product, b),
					Answer:   a,
					Family:   family,
					Kind:     "divide",
					A:        product,
					B:        b,
					Product:  product,
					Operator: "/",
				},
			)
		}
	}
	return uniqueFacts(facts)
}

func uniqueFacts(facts []Fact) []Fact {
	seen := map[string]bool{}
	out := make([]Fact, 0, len(facts))
	for _, fact := range facts {
		if seen[fact.ID] {
			continue
		}
		seen[fact.ID] = true
		out = append(out, fact)
	}
	return out
}

func LoadProgress(path string) (Progress, error) {
	progress := Progress{Facts: map[string]*FactStats{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return progress, nil
		}
		return progress, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return progress, nil
	}
	if err := json.Unmarshal(data, &progress); err != nil {
		return progress, err
	}
	if progress.Facts == nil {
		progress.Facts = map[string]*FactStats{}
	}
	return progress, nil
}

func (t *Trainer) Save() error {
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t.Progress, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.Path, data, 0o644)
}

func (t *Trainer) NextFact() Fact {
	total := 0
	weights := make([]int, len(t.Facts))
	for i, fact := range t.Facts {
		weight := t.Weight(fact.ID)
		weights[i] = weight
		total += weight
	}
	pick := t.Rand.Intn(total)
	for i, weight := range weights {
		if pick < weight {
			return t.Facts[i]
		}
		pick -= weight
	}
	return t.Facts[len(t.Facts)-1]
}

func (t *Trainer) Weight(id string) int {
	stats := t.Progress.Facts[id]
	if stats == nil || stats.Seen == 0 {
		return 8
	}
	avgMS := stats.TotalMS / int64(max(1, stats.Seen))
	weight := 1 + stats.Misses*5 + stats.Wrong*3 + stats.Slow*2
	if avgMS > slowThreshold.Milliseconds() {
		weight += 3
	}
	if stats.Correct >= 5 && stats.Misses == 0 && stats.Slow == 0 {
		weight = 1
	}
	return max(1, weight)
}

func (t *Trainer) Record(id string, answer int, elapsed time.Duration) AttemptResult {
	fact := t.ByID[id]
	stats := t.Progress.Facts[id]
	if stats == nil {
		stats = &FactStats{}
		t.Progress.Facts[id] = stats
	}
	correct := answer == fact.Answer
	slow := elapsed > slowThreshold
	stats.Seen++
	stats.TotalMS += elapsed.Milliseconds()
	stats.Updated = time.Now()
	if correct {
		stats.Correct++
		if slow {
			stats.Slow++
		}
	} else {
		stats.Wrong++
		stats.Misses++
	}
	return AttemptResult{Correct: correct, Slow: slow, Answer: fact.Answer, Fact: fact, Stats: *stats}
}

type FactSummary struct {
	Fact    Fact      `json:"fact"`
	Stats   FactStats `json:"stats"`
	AvgMS   int64     `json:"avg_ms"`
	Weight  int       `json:"weight"`
	Problem string    `json:"problem"`
}

func (t *Trainer) WeakFacts(limit int) []FactSummary {
	var rows []FactSummary
	for _, fact := range t.Facts {
		stats := t.Progress.Facts[fact.ID]
		if stats == nil {
			rows = append(rows, FactSummary{Fact: fact, Weight: t.Weight(fact.ID), Problem: "new"})
			continue
		}
		avg := stats.TotalMS / int64(max(1, stats.Seen))
		problem := ""
		switch {
		case stats.Misses > 0:
			problem = "missed"
		case stats.Slow > 0 || avg > slowThreshold.Milliseconds():
			problem = "slow"
		case stats.Correct >= 5:
			problem = "mastered"
		default:
			problem = "building"
		}
		rows = append(rows, FactSummary{Fact: fact, Stats: *stats, AvgMS: avg, Weight: t.Weight(fact.ID), Problem: problem})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Weight != rows[j].Weight {
			return rows[i].Weight > rows[j].Weight
		}
		return rows[i].Fact.ID < rows[j].Fact.ID
	})
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}
	return rows
}

func (t *Trainer) Totals() map[string]int {
	totals := map[string]int{"correct": 0, "wrong": 0, "seen": 0, "slow": 0}
	for _, stats := range t.Progress.Facts {
		totals["correct"] += stats.Correct
		totals["wrong"] += stats.Wrong
		totals["seen"] += stats.Seen
		totals["slow"] += stats.Slow
	}
	return totals
}

func printWeakFacts(t *Trainer, limit int) {
	fmt.Println("\nWeak facts:")
	for _, row := range t.WeakFacts(limit) {
		if row.Stats.Seen == 0 {
			fmt.Printf("%-9s new\n", row.Fact.Prompt)
			continue
		}
		fmt.Printf("%-9s %-8s correct=%d wrong=%d avg=%0.1fs\n",
			row.Fact.Prompt,
			row.Problem,
			row.Stats.Correct,
			row.Stats.Wrong,
			float64(row.AvgMS)/1000,
		)
	}
}

func defaultProgressPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".math-study-progress.json"
	}
	return filepath.Join(home, ".math-study-progress.json")
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Math Study</title>
<style>
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
body { margin: 0; background: #f7f5ef; color: #202124; }
main { min-height: 100vh; display: grid; grid-template-columns: minmax(320px, 1fr) 360px; gap: 28px; padding: 32px; box-sizing: border-box; }
.practice { display: grid; align-content: center; justify-items: center; gap: 20px; }
h1 { font-size: 18px; margin: 0; font-weight: 650; }
.problem { font-size: clamp(56px, 10vw, 128px); font-weight: 800; line-height: 1; letter-spacing: 0; }
form { display: flex; gap: 10px; width: min(520px, 100%); }
input { flex: 1; font-size: 28px; padding: 14px 16px; border: 2px solid #292929; border-radius: 8px; background: white; }
button { border: 0; border-radius: 8px; background: #1d4f91; color: white; padding: 0 22px; font-size: 18px; font-weight: 700; cursor: pointer; }
.feedback { min-height: 30px; font-size: 20px; font-weight: 650; }
.side { align-self: stretch; border-left: 1px solid #d7d1c3; padding-left: 28px; display: flex; flex-direction: column; gap: 20px; }
.stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.stat { background: white; border: 1px solid #ded8ca; border-radius: 8px; padding: 12px; }
.stat strong { display: block; font-size: 24px; }
.weak { display: grid; gap: 8px; overflow: auto; }
.row { display: grid; grid-template-columns: 1fr auto; gap: 10px; align-items: center; background: white; border: 1px solid #ded8ca; border-radius: 8px; padding: 10px 12px; }
.tag { font-size: 12px; text-transform: uppercase; font-weight: 800; color: #5d6258; }
.meta { color: #62665d; font-size: 13px; }
@media (max-width: 820px) {
  main { grid-template-columns: 1fr; padding: 20px; }
  .practice { min-height: 52vh; }
  .side { border-left: 0; border-top: 1px solid #d7d1c3; padding-left: 0; padding-top: 20px; }
  form { flex-direction: column; }
  button { min-height: 52px; }
}
</style>
</head>
<body>
<main>
  <section class="practice">
    <h1>Arithmetic Trainer</h1>
    <div id="problem" class="problem">...</div>
    <form id="form" autocomplete="off">
      <input id="answer" name="answer" inputmode="numeric" pattern="[0-9]*" autofocus>
      <button>Check</button>
    </form>
    <div id="feedback" class="feedback"></div>
  </section>
  <aside class="side">
    <section>
      <h1>Session</h1>
      <div class="stats">
        <div class="stat"><strong id="seen">0</strong>seen</div>
        <div class="stat"><strong id="correct">0</strong>correct</div>
        <div class="stat"><strong id="slow">0</strong>slow</div>
      </div>
    </section>
    <section>
      <h1>Weak Facts</h1>
      <div id="weak" class="weak"></div>
    </section>
  </aside>
</main>
<script>
let current = null;
let started = 0;
const problem = document.querySelector("#problem");
const answer = document.querySelector("#answer");
const feedback = document.querySelector("#feedback");
const form = document.querySelector("#form");

async function nextFact() {
  const res = await fetch("/api/next");
  current = await res.json();
  problem.textContent = current.prompt;
  answer.value = "";
  answer.focus();
  started = performance.now();
}

async function summary() {
  const res = await fetch("/api/summary");
  const data = await res.json();
  document.querySelector("#seen").textContent = data.totals.seen;
  document.querySelector("#correct").textContent = data.totals.correct;
  document.querySelector("#slow").textContent = data.totals.slow;
  document.querySelector("#weak").innerHTML = data.weak.slice(0, 12).map(row => {
    const avg = row.avg_ms ? (row.avg_ms / 1000).toFixed(1) + "s avg" : "not seen";
    return '<div class="row"><div><strong>' + row.fact.prompt + '</strong><div class="meta">' + avg + '</div></div><span class="tag">' + row.problem + '</span></div>';
  }).join("");
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!current) return;
  const value = parseInt(answer.value, 10);
  if (Number.isNaN(value)) return;
  const ms = Math.round(performance.now() - started);
  const res = await fetch("/api/attempt", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({id: current.id, answer: value, ms})
  });
  const result = await res.json();
  if (result.correct && !result.slow) {
    feedback.textContent = "Correct";
  } else if (result.correct) {
    feedback.textContent = "Correct, but slow";
  } else {
    feedback.textContent = "Missed: " + result.fact.prompt + " = " + result.answer;
  }
  await summary();
  setTimeout(nextFact, result.correct ? 350 : 900);
});

summary();
nextFact();
</script>
</body>
</html>`))
