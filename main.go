package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/big"
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
	ID               string   `json:"id"`
	Prompt           string   `json:"prompt"`
	Answer           string   `json:"answer"`
	Explanation      string   `json:"explanation,omitempty"`
	RelationshipTags []string `json:"relationshipTags,omitempty"`
	Family           string   `json:"family"`
	Kind             string   `json:"kind"`
	A                int      `json:"a"`
	B                int      `json:"b"`
	Product          int      `json:"product"`
	Operator         string   `json:"operator"`
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
	Facts         []Fact
	ByID          map[string]Fact
	Progress      Progress
	Rand          *rand.Rand
	Path          string
	Mode          Mode
	RecentFactIDs []string
}

type Attempt struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
	MS     int64  `json:"ms"`
}

type AttemptResult struct {
	Correct bool      `json:"correct"`
	Slow    bool      `json:"slow"`
	Answer  string    `json:"answer"`
	Fact    Fact      `json:"fact"`
	Stats   FactStats `json:"stats"`
}

type Mode string
type UnitCircleLesson string
type AlgebraIdentityLesson string
type TriangleLesson string

type LessonSelection struct {
	UnitCircle        UnitCircleLesson
	AlgebraIdentities AlgebraIdentityLesson
	Triangles         TriangleLesson
}

const (
	ModeArithmetic        Mode = "arithmetic"
	ModeFractions         Mode = "fractions"
	ModePercentages       Mode = "percentages"
	ModePercentRelations  Mode = "percent-relations"
	ModeUnitCircle        Mode = "unit-circle"
	ModeExponentsLogs     Mode = "exponents-logs"
	ModeAlgebraIdentities Mode = "algebra-identities"
	ModeTriangles         Mode = "triangles"
	ModeRelationships     Mode = "relationships"
	ModeMixed             Mode = "mixed"
)

const (
	UnitCircleLessonConcepts        UnitCircleLesson = "concepts"
	UnitCircleLessonQuadrants       UnitCircleLesson = "quadrants"
	UnitCircleLessonReferenceAngles UnitCircleLesson = "reference-angles"
	UnitCircleLessonReferenceValues UnitCircleLesson = "reference-values"
	UnitCircleLessonRadians         UnitCircleLesson = "radians"
	UnitCircleLessonAssemble        UnitCircleLesson = "assemble"
	UnitCircleLessonTangent         UnitCircleLesson = "tangent"
	UnitCircleLessonReciprocals     UnitCircleLesson = "reciprocals"
	UnitCircleLessonMixed           UnitCircleLesson = "mixed"
)

const (
	AlgebraIdentityLessonConcepts  AlgebraIdentityLesson = "concepts"
	AlgebraIdentityLessonExpand    AlgebraIdentityLesson = "expand"
	AlgebraIdentityLessonFactor    AlgebraIdentityLesson = "factor"
	AlgebraIdentityLessonRecognize AlgebraIdentityLesson = "recognize"
	AlgebraIdentityLessonMixed     AlgebraIdentityLesson = "mixed"
)

const (
	TriangleLessonConcepts    TriangleLesson = "concepts"
	TriangleLessonAngleSum    TriangleLesson = "angle-sum"
	TriangleLessonPythagorean TriangleLesson = "pythagorean"
	TriangleLessonSpecial     TriangleLesson = "special-right"
	TriangleLessonSOHCAHTOA   TriangleLesson = "sohcahtoa"
	TriangleLessonMixed       TriangleLesson = "mixed"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "web":
			webCmd(os.Args[2:])
			return
		case "shell", "interactive", "menu":
			shellCmd(os.Args[2:])
			return
		}
	}
	cliCmd(os.Args[1:])
}

func cliCmd(args []string) {
	fs := flag.NewFlagSet("math-study", flag.ExitOnError)
	min := fs.Int("min", 2, "smallest multiplication factor")
	max := fs.Int("max", 12, "largest multiplication factor")
	mode := fs.String("mode", string(ModeArithmetic), "practice mode: arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, algebra-identities, triangles, relationships, mixed")
	lesson := fs.String("lesson", string(UnitCircleLessonConcepts), "lesson for unit-circle, algebra-identities, or triangles")
	minutes := fs.Int("minutes", 10, "session length in minutes")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	parsedMode := parseMode(*mode)
	parsedLessons := lessonsForMode(parsedMode, *lesson)
	trainer := mustTrainer(*min, *max, parsedMode, parsedLessons, *progress)
	reader := bufio.NewReader(os.Stdin)
	runPracticeSession(trainer, reader, *minutes)
}

func runPracticeSession(trainer *Trainer, reader *bufio.Reader, minutes int) {
	deadline := time.Now().Add(time.Duration(minutes) * time.Minute)
	fmt.Printf("Math trainer: %s mode. Type q to quit.\n", trainer.Mode)
	for time.Now().Before(deadline) {
		fact := trainer.NextFact()
		start := time.Now()
		fmt.Printf("%s = ", fact.Prompt)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "q") || strings.EqualFold(line, "quit") {
			break
		}
		result := trainer.Record(fact.ID, line, time.Since(start))
		if result.Correct && !result.Slow {
			fmt.Println("correct")
		} else if result.Correct {
			fmt.Printf("correct, but slow (%0.1fs)\n", float64(time.Since(start).Milliseconds())/1000)
		} else {
			fmt.Printf("missed: %s = %s\n", fact.Prompt, fact.Answer)
			if fact.Explanation != "" {
				fmt.Printf("why: %s\n", fact.Explanation)
			}
		}
	}
	if err := trainer.Save(); err != nil {
		log.Printf("save progress: %v", err)
	}
	printWeakFacts(trainer, 12)
}

type shellModeChoice struct {
	Mode        Mode
	Label       string
	Description string
	Aliases     []string
}

type shellLessonChoice struct {
	Value       string
	Label       string
	Description string
}

func shellCmd(args []string) {
	fs := flag.NewFlagSet("math-study shell", flag.ExitOnError)
	minDefault := fs.Int("min", 2, "default smallest multiplication factor")
	maxDefault := fs.Int("max", 12, "default largest multiplication factor")
	minutesDefault := fs.Int("minutes", 10, "default session length in minutes")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Math Relationship Trainer")
	fmt.Println("Interactive shell. Type q at any menu prompt to quit.")
	fmt.Printf("Progress: %s\n", *progress)

	for {
		mode, ok := promptShellMode(reader)
		if !ok {
			return
		}
		lesson := ""
		if shellModeHasLessons(mode) {
			lesson, ok = promptShellLesson(reader, mode)
			if !ok {
				return
			}
		}
		minValue := *minDefault
		maxValue := *maxDefault
		if shellModeUsesArithmeticRange(mode) {
			minValue, ok = promptShellInt(reader, "Smallest multiplication factor", *minDefault, 1, 99)
			if !ok {
				return
			}
			maxValue, ok = promptShellInt(reader, "Largest multiplication factor", *maxDefault, minValue, 99)
			if !ok {
				return
			}
		}
		minutes, ok := promptShellInt(reader, "Session length in minutes", *minutesDefault, 1, 240)
		if !ok {
			return
		}

		lessons := lessonsForMode(mode, lesson)
		trainer := mustTrainer(minValue, maxValue, mode, lessons, *progress)
		fmt.Println()
		printShellSessionSummary(mode, lessons, minValue, maxValue, minutes)
		runPracticeSession(trainer, reader, minutes)

		again, ok := promptShellYesNo(reader, "\nStart another session?", true)
		if !ok || !again {
			return
		}
	}
}

func promptShellMode(reader *bufio.Reader) (Mode, bool) {
	fmt.Println("\nModes:")
	for i, choice := range shellModeChoices() {
		fmt.Printf("  %d. %-20s %s\n", i+1, choice.Label, choice.Description)
	}
	for {
		input, ok := promptShellLine(reader, fmt.Sprintf("Mode [%s]: ", ModeRelationships))
		if !ok {
			return "", false
		}
		mode, ok := parseShellModeChoice(input)
		if ok {
			return mode, true
		}
		fmt.Println("Enter a mode number or name from the list.")
	}
}

func promptShellLesson(reader *bufio.Reader, mode Mode) (string, bool) {
	choices := shellLessonChoices(mode)
	if len(choices) == 0 {
		return "", true
	}
	fmt.Println("\nLessons:")
	for i, choice := range choices {
		fmt.Printf("  %d. %-18s %s\n", i+1, choice.Label, choice.Description)
	}
	for {
		input, ok := promptShellLine(reader, fmt.Sprintf("Lesson [%s]: ", choices[0].Value))
		if !ok {
			return "", false
		}
		lesson, ok := parseShellLessonChoice(mode, input)
		if ok {
			return lesson, true
		}
		fmt.Println("Enter a lesson number or name from the list.")
	}
}

func promptShellInt(reader *bufio.Reader, label string, defaultValue, minValue, maxValue int) (int, bool) {
	for {
		input, ok := promptShellLine(reader, fmt.Sprintf("%s [%d]: ", label, defaultValue))
		if !ok {
			return 0, false
		}
		if input == "" {
			return defaultValue, true
		}
		value, err := strconv.Atoi(input)
		if err != nil || value < minValue || value > maxValue {
			fmt.Printf("Enter a whole number from %d to %d.\n", minValue, maxValue)
			continue
		}
		return value, true
	}
}

func promptShellYesNo(reader *bufio.Reader, label string, defaultValue bool) (bool, bool) {
	defaultText := "Y/n"
	if !defaultValue {
		defaultText = "y/N"
	}
	for {
		input, ok := promptShellLine(reader, fmt.Sprintf("%s [%s]: ", label, defaultText))
		if !ok {
			return false, false
		}
		if input == "" {
			return defaultValue, true
		}
		switch strings.ToLower(input) {
		case "y", "yes":
			return true, true
		case "n", "no":
			return false, true
		default:
			fmt.Println("Enter y or n.")
		}
	}
}

func promptShellLine(reader *bufio.Reader, prompt string) (string, bool) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", false
	}
	input := strings.TrimSpace(line)
	if strings.EqualFold(input, "q") || strings.EqualFold(input, "quit") || strings.EqualFold(input, "exit") {
		return "", false
	}
	return input, true
}

func parseShellModeChoice(input string) (Mode, bool) {
	input = normalizeShellChoice(input)
	if input == "" {
		return ModeRelationships, true
	}
	if n, err := strconv.Atoi(input); err == nil {
		choices := shellModeChoices()
		if n >= 1 && n <= len(choices) {
			return choices[n-1].Mode, true
		}
		return "", false
	}
	for _, choice := range shellModeChoices() {
		if normalizeShellChoice(string(choice.Mode)) == input || normalizeShellChoice(choice.Label) == input {
			return choice.Mode, true
		}
		for _, alias := range choice.Aliases {
			if normalizeShellChoice(alias) == input {
				return choice.Mode, true
			}
		}
	}
	return "", false
}

func parseShellLessonChoice(mode Mode, input string) (string, bool) {
	choices := shellLessonChoices(mode)
	if len(choices) == 0 {
		return "", input == ""
	}
	input = normalizeShellChoice(input)
	if input == "" {
		return choices[0].Value, true
	}
	if n, err := strconv.Atoi(input); err == nil {
		if n >= 1 && n <= len(choices) {
			return choices[n-1].Value, true
		}
		return "", false
	}
	for _, choice := range choices {
		if normalizeShellChoice(choice.Value) == input || normalizeShellChoice(choice.Label) == input {
			return choice.Value, true
		}
	}
	return "", false
}

func normalizeShellChoice(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Join(strings.Fields(value), "-")
	return value
}

func shellModeUsesArithmeticRange(mode Mode) bool {
	return mode == ModeArithmetic || mode == ModeMixed
}

func shellModeHasLessons(mode Mode) bool {
	return len(shellLessonChoices(mode)) > 0
}

func shellModeChoices() []shellModeChoice {
	return []shellModeChoice{
		{Mode: ModeArithmetic, Label: "arithmetic", Description: "multiplication/division facts", Aliases: []string{"facts", "multiplication", "division"}},
		{Mode: ModeFractions, Label: "fractions", Description: "fractions, decimals, and parts-of-a-whole", Aliases: []string{"fraction-relations", "fraction-relationships"}},
		{Mode: ModePercentages, Label: "percentages", Description: "percent, decimal, and fraction equivalence", Aliases: []string{"percents"}},
		{Mode: ModePercentRelations, Label: "percent-relations", Description: "a% of b = b% of a", Aliases: []string{"percentage-relations", "percentage-relationships", "percentrelationships", "percentrelations"}},
		{Mode: ModeUnitCircle, Label: "unit-circle", Description: "staged unit-circle relationships", Aliases: []string{"unitcircle", "circle", "trig", "trigonometry"}},
		{Mode: ModeExponentsLogs, Label: "exponents-logs", Description: "root, exponent, and log relationships", Aliases: []string{"exponents", "logs", "log-relations", "root-exponent-log", "roots-exponents-logs"}},
		{Mode: ModeAlgebraIdentities, Label: "algebra-identities", Description: "algebra patterns and transformations", Aliases: []string{"identities", "algebra-patterns"}},
		{Mode: ModeTriangles, Label: "triangles", Description: "triangle relationships for trig foundations", Aliases: []string{"geometry-triangles", "trig-triangles"}},
		{Mode: ModeRelationships, Label: "relationships", Description: "all conceptual decks without arithmetic", Aliases: []string{"relations", "math-relations", "math-relationships", "relationship-trainer"}},
		{Mode: ModeMixed, Label: "mixed", Description: "everything, including arithmetic"},
	}
}

func shellLessonChoices(mode Mode) []shellLessonChoice {
	switch mode {
	case ModeUnitCircle:
		return []shellLessonChoice{
			{Value: string(UnitCircleLessonConcepts), Label: "concepts", Description: "(cos, sin), sine-y, cosine-x, tangent-y/x"},
			{Value: string(UnitCircleLessonQuadrants), Label: "quadrants", Description: "quadrants and signs"},
			{Value: string(UnitCircleLessonReferenceAngles), Label: "reference-angles", Description: "reference angles and quadrants"},
			{Value: string(UnitCircleLessonReferenceValues), Label: "reference-values", Description: "30/45/60 first-quadrant values"},
			{Value: string(UnitCircleLessonRadians), Label: "radians", Description: "degree/radian conversions"},
			{Value: string(UnitCircleLessonAssemble), Label: "assemble", Description: "reference angle plus sign for sin/cos"},
			{Value: string(UnitCircleLessonTangent), Label: "tangent", Description: "tan ratios, values, and undefined cases"},
			{Value: string(UnitCircleLessonReciprocals), Label: "reciprocals", Description: "sec, csc, and cot relationships"},
			{Value: string(UnitCircleLessonMixed), Label: "mixed", Description: "full unit-circle review"},
		}
	case ModeAlgebraIdentities:
		return []shellLessonChoice{
			{Value: string(AlgebraIdentityLessonConcepts), Label: "concepts", Description: "identity, expand, factor, equivalent expression"},
			{Value: string(AlgebraIdentityLessonExpand), Label: "expand", Description: "forward expansion patterns"},
			{Value: string(AlgebraIdentityLessonFactor), Label: "factor", Description: "reverse factoring patterns"},
			{Value: string(AlgebraIdentityLessonRecognize), Label: "recognize", Description: "name the matching pattern"},
			{Value: string(AlgebraIdentityLessonMixed), Label: "mixed", Description: "full algebra identity review"},
		}
	case ModeTriangles:
		return []shellLessonChoice{
			{Value: string(TriangleLessonConcepts), Label: "concepts", Description: "right angle, hypotenuse, opposite, adjacent"},
			{Value: string(TriangleLessonAngleSum), Label: "angle-sum", Description: "180 degree sum and missing angles"},
			{Value: string(TriangleLessonPythagorean), Label: "pythagorean", Description: "a^2+b^2=c^2 and missing sides"},
			{Value: string(TriangleLessonSpecial), Label: "special-right", Description: "30-60-90 and 45-45-90 ratios"},
			{Value: string(TriangleLessonSOHCAHTOA), Label: "sohcahtoa", Description: "sin, cos, tan side ratios"},
			{Value: string(TriangleLessonMixed), Label: "mixed", Description: "full triangle review"},
		}
	default:
		return nil
	}
}

func printShellSessionSummary(mode Mode, lessons LessonSelection, minValue, maxValue, minutes int) {
	fmt.Printf("Starting %d minute %s session", minutes, mode)
	switch mode {
	case ModeUnitCircle:
		fmt.Printf(" (%s lesson)", lessons.UnitCircle)
	case ModeAlgebraIdentities:
		fmt.Printf(" (%s lesson)", lessons.AlgebraIdentities)
	case ModeTriangles:
		fmt.Printf(" (%s lesson)", lessons.Triangles)
	}
	if shellModeUsesArithmeticRange(mode) {
		fmt.Printf(" with arithmetic range %d-%d", minValue, maxValue)
	}
	fmt.Println(".")
}

func webCmd(args []string) {
	fs := flag.NewFlagSet("math-study web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	min := fs.Int("min", 2, "smallest multiplication factor")
	max := fs.Int("max", 12, "largest multiplication factor")
	mode := fs.String("mode", string(ModeArithmetic), "practice mode: arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, algebra-identities, triangles, relationships, mixed")
	lesson := fs.String("lesson", string(UnitCircleLessonConcepts), "lesson for unit-circle, algebra-identities, or triangles")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	parsedMode := parseMode(*mode)
	parsedLessons := lessonsForMode(parsedMode, *lesson)
	trainer := mustTrainer(*min, *max, parsedMode, parsedLessons, *progress)
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

func mustTrainer(min, max int, mode Mode, lessons LessonSelection, path string) *Trainer {
	if min < 1 || max < min {
		log.Fatalf("invalid range: min=%d max=%d", min, max)
	}
	trainer, err := NewTrainerWithLessons(min, max, mode, lessons, path)
	if err != nil {
		log.Fatal(err)
	}
	return trainer
}

func parseMode(mode string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(mode))) {
	case ModeArithmetic, ModeFractions, ModePercentages, ModePercentRelations, ModeUnitCircle, ModeExponentsLogs, ModeAlgebraIdentities, ModeTriangles, ModeRelationships, ModeMixed:
		return Mode(strings.ToLower(strings.TrimSpace(mode)))
	case "fraction-relations", "fraction-relationships":
		return ModeFractions
	case "percentage-relations", "percentage-relationships", "percentrelationships":
		return ModePercentRelations
	case "percentrelations":
		return ModePercentRelations
	case "unitcircle", "circle", "trig", "trigonometry":
		return ModeUnitCircle
	case "exponents", "exponent", "logs", "log", "logarithms", "logarithm", "exponent-logs", "exponentslogs", "log-relations", "exponent-relations", "roots-exponents-logs", "root-exponent-log", "root-exponent-logs", "root-log-relations":
		return ModeExponentsLogs
	case "identities", "algebra-patterns":
		return ModeAlgebraIdentities
	case "geometry-triangles", "trig-triangles":
		return ModeTriangles
	case "relations", "math-relations", "math-relationships", "relationship-trainer":
		return ModeRelationships
	default:
		log.Fatalf("unknown mode %q; use arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, algebra-identities, triangles, relationships, or mixed", mode)
		return ModeArithmetic
	}
}

func lessonForMode(mode Mode, lesson string) UnitCircleLesson {
	if mode != ModeUnitCircle {
		return UnitCircleLessonMixed
	}
	return parseUnitCircleLesson(lesson)
}

func lessonsForMode(mode Mode, lesson string) LessonSelection {
	return LessonSelection{
		UnitCircle:        lessonForMode(mode, lesson),
		AlgebraIdentities: algebraIdentityLessonForMode(mode, lesson),
		Triangles:         triangleLessonForMode(mode, lesson),
	}
}

func algebraIdentityLessonForMode(mode Mode, lesson string) AlgebraIdentityLesson {
	if mode != ModeAlgebraIdentities {
		return AlgebraIdentityLessonMixed
	}
	return parseAlgebraIdentityLesson(lesson)
}

func triangleLessonForMode(mode Mode, lesson string) TriangleLesson {
	if mode != ModeTriangles {
		return TriangleLessonMixed
	}
	return parseTriangleLesson(lesson)
}

func parseUnitCircleLesson(lesson string) UnitCircleLesson {
	switch UnitCircleLesson(strings.ToLower(strings.TrimSpace(lesson))) {
	case "", UnitCircleLessonConcepts:
		return UnitCircleLessonConcepts
	case UnitCircleLessonQuadrants:
		return UnitCircleLessonQuadrants
	case UnitCircleLessonReferenceAngles:
		return UnitCircleLessonReferenceAngles
	case UnitCircleLessonReferenceValues:
		return UnitCircleLessonReferenceValues
	case UnitCircleLessonRadians:
		return UnitCircleLessonRadians
	case UnitCircleLessonAssemble:
		return UnitCircleLessonAssemble
	case UnitCircleLessonTangent:
		return UnitCircleLessonTangent
	case UnitCircleLessonReciprocals:
		return UnitCircleLessonReciprocals
	case UnitCircleLessonMixed:
		return UnitCircleLessonMixed
	default:
		log.Fatalf("unknown unit-circle lesson %q; use concepts, quadrants, reference-angles, reference-values, radians, assemble, tangent, reciprocals, or mixed", lesson)
		return UnitCircleLessonConcepts
	}
}

func parseAlgebraIdentityLesson(lesson string) AlgebraIdentityLesson {
	switch AlgebraIdentityLesson(strings.ToLower(strings.TrimSpace(lesson))) {
	case "", AlgebraIdentityLessonConcepts:
		return AlgebraIdentityLessonConcepts
	case AlgebraIdentityLessonExpand:
		return AlgebraIdentityLessonExpand
	case AlgebraIdentityLessonFactor:
		return AlgebraIdentityLessonFactor
	case AlgebraIdentityLessonRecognize:
		return AlgebraIdentityLessonRecognize
	case AlgebraIdentityLessonMixed:
		return AlgebraIdentityLessonMixed
	default:
		log.Fatalf("unknown algebra-identities lesson %q; use concepts, expand, factor, recognize, or mixed", lesson)
		return AlgebraIdentityLessonConcepts
	}
}

func parseTriangleLesson(lesson string) TriangleLesson {
	switch TriangleLesson(strings.ToLower(strings.TrimSpace(lesson))) {
	case "", TriangleLessonConcepts:
		return TriangleLessonConcepts
	case TriangleLessonAngleSum:
		return TriangleLessonAngleSum
	case TriangleLessonPythagorean:
		return TriangleLessonPythagorean
	case TriangleLessonSpecial:
		return TriangleLessonSpecial
	case TriangleLessonSOHCAHTOA:
		return TriangleLessonSOHCAHTOA
	case TriangleLessonMixed:
		return TriangleLessonMixed
	default:
		log.Fatalf("unknown triangles lesson %q; use concepts, angle-sum, pythagorean, special-right, sohcahtoa, or mixed", lesson)
		return TriangleLessonConcepts
	}
}

func NewTrainer(min, max int, mode Mode, path string) (*Trainer, error) {
	return NewTrainerWithLesson(min, max, mode, UnitCircleLessonMixed, path)
}

func NewTrainerWithLesson(min, max int, mode Mode, lesson UnitCircleLesson, path string) (*Trainer, error) {
	return NewTrainerWithLessons(min, max, mode, LessonSelection{
		UnitCircle:        lesson,
		AlgebraIdentities: AlgebraIdentityLessonMixed,
		Triangles:         TriangleLessonMixed,
	}, path)
}

func NewTrainerWithLessons(min, max int, mode Mode, lessons LessonSelection, path string) (*Trainer, error) {
	facts := BuildFactsWithLessons(min, max, mode, lessons)
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
		Mode:     mode,
	}, nil
}

func BuildFacts(min, max int, mode Mode) []Fact {
	return BuildFactsWithLesson(min, max, mode, UnitCircleLessonMixed)
}

func BuildFactsWithLesson(min, max int, mode Mode, lesson UnitCircleLesson) []Fact {
	return BuildFactsWithLessons(min, max, mode, LessonSelection{
		UnitCircle:        lesson,
		AlgebraIdentities: AlgebraIdentityLessonMixed,
		Triangles:         TriangleLessonMixed,
	})
}

func BuildFactsWithLessons(min, max int, mode Mode, lessons LessonSelection) []Fact {
	var facts []Fact
	if mode == ModeArithmetic || mode == ModeMixed {
		facts = append(facts, BuildArithmeticFacts(min, max)...)
	}
	if mode == ModeFractions || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildFractionFacts()...)
	}
	if mode == ModePercentages || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildPercentageFacts()...)
	}
	if mode == ModePercentRelations || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildPercentageRelationFacts()...)
	}
	if mode == ModeUnitCircle {
		facts = append(facts, BuildUnitCircleLessonFacts(lessons.UnitCircle)...)
	} else if mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildUnitCircleFacts()...)
	}
	if mode == ModeExponentsLogs || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildExponentLogFacts()...)
	}
	if mode == ModeAlgebraIdentities {
		facts = append(facts, BuildAlgebraIdentityLessonFacts(lessons.AlgebraIdentities)...)
	} else if mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildAlgebraIdentityFacts()...)
	}
	if mode == ModeTriangles {
		facts = append(facts, BuildTriangleLessonFacts(lessons.Triangles)...)
	} else if mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildTriangleFacts()...)
	}
	return uniqueFacts(facts)
}

func BuildArithmeticFacts(min, max int) []Fact {
	var facts []Fact
	for a := min; a <= max; a++ {
		for b := a; b <= max; b++ {
			product := a * b
			family := fmt.Sprintf("%dx%d=%d", a, b, product)
			facts = append(facts,
				Fact{
					ID:       fmt.Sprintf("mul:%d:%d", a, b),
					Prompt:   fmt.Sprintf("%d x %d", a, b),
					Answer:   strconv.Itoa(product),
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
					Answer:   strconv.Itoa(product),
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
					Answer:   strconv.Itoa(b),
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
					Answer:   strconv.Itoa(a),
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
	return facts
}

func BuildFractionFacts() []Fact {
	denominators := []int{2, 4, 5, 8, 10, 16}
	var facts []Fact
	for _, denominator := range denominators {
		for numerator := 1; numerator < denominator; numerator++ {
			if gcd(numerator, denominator) != 1 {
				continue
			}
			decimal := decimalForFraction(numerator, denominator)
			fraction := fmt.Sprintf("%d/%d", numerator, denominator)
			family := fmt.Sprintf("%s=%s", fraction, decimal)
			complement := reducedFractionString(denominator-numerator, denominator)
			reciprocal := fmt.Sprintf("%d/%d", denominator, numerator)
			partitionExplanation := fmt.Sprintf("%s means %d selected part(s) out of %d equal part(s) of one whole.", fraction, numerator, denominator)
			decimalExplanation := fmt.Sprintf("%s is the same value as %s because a fraction is division: %d divided by %d.", fraction, decimal, numerator, denominator)
			facts = append(facts,
				Fact{
					ID:               fmt.Sprintf("frac2dec:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as decimal", fraction),
					Answer:           decimal,
					Explanation:      decimalExplanation,
					RelationshipTags: []string{"fraction-decimal-equivalence", "fraction-as-division", "parts-of-a-whole"},
					Family:           family,
					Kind:             "fraction_to_decimal",
					A:                numerator,
					B:                denominator,
					Operator:         "/",
				},
				Fact{
					ID:               fmt.Sprintf("dec2frac:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as fraction", decimal),
					Answer:           fraction,
					Explanation:      decimalExplanation,
					RelationshipTags: []string{"fraction-decimal-equivalence", "fraction-as-division", "parts-of-a-whole"},
					Family:           family,
					Kind:             "decimal_to_fraction",
					A:                numerator,
					B:                denominator,
					Operator:         "=",
				},
				Fact{
					ID:               fmt.Sprintf("frac:numerator:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("In %s, how many equal parts are selected?", fraction),
					Answer:           strconv.Itoa(numerator),
					Explanation:      partitionExplanation,
					RelationshipTags: []string{"parts-of-a-whole", "numerator-denominator-roles"},
					Family:           family,
					Kind:             "exact_quantity",
					A:                numerator,
					B:                denominator,
					Operator:         "/",
				},
				Fact{
					ID:               fmt.Sprintf("frac:denominator:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("In %s, the whole is split into how many equal parts?", fraction),
					Answer:           strconv.Itoa(denominator),
					Explanation:      partitionExplanation,
					RelationshipTags: []string{"parts-of-a-whole", "numerator-denominator-roles"},
					Family:           family,
					Kind:             "exact_quantity",
					A:                numerator,
					B:                denominator,
					Operator:         "/",
				},
				Fact{
					ID:               fmt.Sprintf("frac:complement:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s + ? = 1", fraction),
					Answer:           complement,
					Explanation:      fmt.Sprintf("Complements to 1 use the same whole: %s leaves %s because %d of %d parts remain.", fraction, complement, denominator-numerator, denominator),
					RelationshipTags: []string{"complements-to-one", "parts-of-a-whole", "equivalent-values"},
					Family:           family,
					Kind:             "exact_quantity",
					A:                numerator,
					B:                denominator,
					Operator:         "+",
				},
				Fact{
					ID:               fmt.Sprintf("frac:reciprocal:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("reciprocal of %s", fraction),
					Answer:           reciprocal,
					Explanation:      fmt.Sprintf("A reciprocal swaps numerator and denominator; %s becomes %s.", fraction, reciprocal),
					RelationshipTags: []string{"reciprocal-relationships", "numerator-denominator-roles"},
					Family:           family,
					Kind:             "exact_quantity",
					A:                numerator,
					B:                denominator,
					Operator:         "/",
				},
			)
		}
	}
	return facts
}

func BuildPercentageFacts() []Fact {
	denominators := []int{2, 4, 5, 8, 10, 16}
	var facts []Fact
	for _, denominator := range denominators {
		for numerator := 1; numerator < denominator; numerator++ {
			if gcd(numerator, denominator) != 1 {
				continue
			}
			decimal := decimalForFraction(numerator, denominator)
			fraction := fmt.Sprintf("%d/%d", numerator, denominator)
			percent := percentForFraction(numerator, denominator)
			family := fmt.Sprintf("%s=%s=%s", fraction, decimal, percent)
			perHundred := strings.TrimSuffix(percent, "%")
			explanation := fmt.Sprintf("%s, %s, and %s are the same value. Percent means per 100, and as an operator %s means multiply by %s.", fraction, decimal, percent, percent, decimal)
			facts = append(facts,
				Fact{
					ID:               fmt.Sprintf("frac2pct:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as percent", fraction),
					Answer:           percent,
					Explanation:      explanation,
					RelationshipTags: []string{"percent-means-per-100", "fraction-percent-equivalence", "percent-as-multiplier"},
					Family:           family,
					Kind:             "fraction_to_percent",
					A:                numerator,
					B:                denominator,
					Operator:         "%",
				},
				Fact{
					ID:               fmt.Sprintf("pct2frac:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as fraction", percent),
					Answer:           fraction,
					Explanation:      explanation,
					RelationshipTags: []string{"percent-means-per-100", "fraction-percent-equivalence", "percent-as-multiplier"},
					Family:           family,
					Kind:             "percent_to_fraction",
					A:                numerator,
					B:                denominator,
					Operator:         "%",
				},
				Fact{
					ID:               fmt.Sprintf("dec2pct:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as percent", decimal),
					Answer:           percent,
					Explanation:      explanation,
					RelationshipTags: []string{"decimal-percent-equivalence", "percent-means-per-100", "percent-as-multiplier"},
					Family:           family,
					Kind:             "decimal_to_percent",
					A:                numerator,
					B:                denominator,
					Operator:         "%",
				},
				Fact{
					ID:               fmt.Sprintf("pct2dec:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s as multiplier", percent),
					Answer:           decimal,
					Explanation:      explanation,
					RelationshipTags: []string{"decimal-percent-equivalence", "percent-means-per-100", "percent-as-multiplier"},
					Family:           family,
					Kind:             "percent_to_decimal",
					A:                numerator,
					B:                denominator,
					Operator:         "%",
				},
				Fact{
					ID:               fmt.Sprintf("pct:per100:%d:%d", numerator, denominator),
					Prompt:           fmt.Sprintf("%s means how many per 100?", percent),
					Answer:           perHundred,
					Explanation:      fmt.Sprintf("Percent literally means per 100, so %s means %s out of 100.", percent, perHundred),
					RelationshipTags: []string{"percent-means-per-100"},
					Family:           family,
					Kind:             "exact_quantity",
					A:                numerator,
					B:                denominator,
					Operator:         "%",
				},
			)
		}
	}
	return facts
}

func BuildPercentageRelationFacts() []Fact {
	values := []int{4, 5, 8, 10, 12, 15, 16, 20, 24, 25, 30, 35, 40, 50, 60, 75, 80, 100}
	var facts []Fact
	for _, percent := range values {
		for _, base := range values {
			if percent == base {
				continue
			}
			result := decimalForFraction(percent*base, 100)
			family := fmt.Sprintf("%d%% of %d=%d%% of %d=%s", percent, base, base, percent, result)
			facts = append(facts, Fact{
				ID:               fmt.Sprintf("pctrel:%d:%d", percent, base),
				Prompt:           fmt.Sprintf("%d%% of %d", percent, base),
				Answer:           fmt.Sprintf("%d%% of %d", base, percent),
				Explanation:      fmt.Sprintf("%d%% of %d means (%d/100) x %d. Multiplication commutes, so it equals %d%% of %d.", percent, base, percent, base, base, percent),
				RelationshipTags: []string{"percent-of-swap", "commutative-multiplication", "percent-as-multiplier"},
				Family:           family,
				Kind:             "percent_relationship",
				A:                percent,
				B:                base,
				Product:          percent * base,
				Operator:         "%",
			})
		}
	}
	return facts
}

type UnitCircleAngle struct {
	Degrees int
	Radians string
	Sin     string
	Cos     string
	Tan     string
}

func BuildUnitCircleFacts() []Fact {
	angles := unitCircleAngles()
	var facts []Fact
	for _, angle := range angles {
		degrees := fmt.Sprintf("%d deg", angle.Degrees)
		point := fmt.Sprintf("(%s,%s)", angle.Cos, angle.Sin)
		family := fmt.Sprintf("%s=%s point=%s", degrees, angle.Radians, point)
		degreeExplanation := fmt.Sprintf("Degrees and radians name the same angle; 180 deg equals pi radians, so %d deg equals %s.", angle.Degrees, angle.Radians)
		pointExplanation := fmt.Sprintf("Unit-circle points are (cos theta, sin theta), so at %s the point is %s.", angle.Radians, point)
		sinExplanation := fmt.Sprintf("Unit-circle points are (cos theta, sin theta), so sine is the y-coordinate: %s.", angle.Sin)
		cosExplanation := fmt.Sprintf("Unit-circle points are (cos theta, sin theta), so cosine is the x-coordinate: %s.", angle.Cos)
		tanExplanation := fmt.Sprintf("Tangent is y/x, so tan(theta) = sin(theta)/cos(theta) = %s/%s.", angle.Sin, angle.Cos)
		facts = append(facts,
			unitCircleFact(fmt.Sprintf("uc:deg2rad:%d", angle.Degrees), fmt.Sprintf("%s in radians", degrees), angle.Radians, family, "unit_circle_radian", "=", degreeExplanation, "radian-degree-conversion"),
			unitCircleFact(fmt.Sprintf("uc:rad2deg:%d", angle.Degrees), fmt.Sprintf("%s in degrees", angle.Radians), degrees, family, "unit_circle_degree", "=", degreeExplanation, "radian-degree-conversion", "angle-to-degree"),
			unitCircleFact(fmt.Sprintf("uc:sin:deg:%d", angle.Degrees), fmt.Sprintf("sin(%s)", degrees), angle.Sin, family, "unit_circle_value", "sin", sinExplanation, "angle-to-coordinate", "sin-is-y"),
			unitCircleFact(fmt.Sprintf("uc:sin:rad:%d", angle.Degrees), fmt.Sprintf("sin(%s)", angle.Radians), angle.Sin, family, "unit_circle_value", "sin", sinExplanation, "angle-to-coordinate", "sin-is-y"),
			unitCircleFact(fmt.Sprintf("uc:cos:deg:%d", angle.Degrees), fmt.Sprintf("cos(%s)", degrees), angle.Cos, family, "unit_circle_value", "cos", cosExplanation, "angle-to-coordinate", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:cos:rad:%d", angle.Degrees), fmt.Sprintf("cos(%s)", angle.Radians), angle.Cos, family, "unit_circle_value", "cos", cosExplanation, "angle-to-coordinate", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:tan:deg:%d", angle.Degrees), fmt.Sprintf("tan(%s)", degrees), angle.Tan, family, "unit_circle_value", "tan", tanExplanation, "tan-is-y-over-x", "angle-to-coordinate"),
			unitCircleFact(fmt.Sprintf("uc:tan:rad:%d", angle.Degrees), fmt.Sprintf("tan(%s)", angle.Radians), angle.Tan, family, "unit_circle_value", "tan", tanExplanation, "tan-is-y-over-x", "angle-to-coordinate"),
			unitCircleFact(fmt.Sprintf("uc:point:deg:%d", angle.Degrees), fmt.Sprintf("point at %s", degrees), point, family, "unit_circle_point", "=", pointExplanation, "angle-to-coordinate", "sin-is-y", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:point:rad:%d", angle.Degrees), fmt.Sprintf("point at %s", angle.Radians), point, family, "unit_circle_point", "=", pointExplanation, "angle-to-coordinate", "sin-is-y", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:point-sin:%d", angle.Degrees), fmt.Sprintf("At %s, point is %s. What is sin?", angle.Radians, point), angle.Sin, family, "unit_circle_value", "sin", sinExplanation, "angle-to-coordinate", "sin-is-y"),
			unitCircleFact(fmt.Sprintf("uc:point-cos:%d", angle.Degrees), fmt.Sprintf("At %s, point is %s. What is cos?", angle.Radians, point), angle.Cos, family, "unit_circle_value", "cos", cosExplanation, "angle-to-coordinate", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:point-tan:%d", angle.Degrees), fmt.Sprintf("At %s, point is %s. What is tan?", angle.Radians, point), angle.Tan, family, "unit_circle_value", "tan", tanExplanation, "angle-to-coordinate", "tan-is-y-over-x"),
		)
		if quadrant, ok := quadrantForDegrees(angle.Degrees); ok {
			refDegrees := referenceAngle(angle.Degrees)
			refRadians := radiansForDegrees(refDegrees)
			triangle := referenceTriangle(refDegrees)
			quadrantExplanation := fmt.Sprintf("%s is in quadrant %s. Quadrants come from the signs of the x and y coordinates.", angle.Radians, quadrant)
			refExplanation := fmt.Sprintf("The reference angle is the acute angle to the x-axis; %s has reference angle %s.", angle.Radians, refRadians)
			facts = append(facts,
				unitCircleFact(fmt.Sprintf("uc:quadrant:%d", angle.Degrees), fmt.Sprintf("quadrant for %s", degrees), quadrant, family, "unit_circle_quadrant", "=", quadrantExplanation, "quadrant-signs"),
				unitCircleFact(fmt.Sprintf("uc:ref:%d", angle.Degrees), fmt.Sprintf("reference angle for %s", degrees), fmt.Sprintf("%d deg", refDegrees), family, "unit_circle_degree", "=", refExplanation, "reference-angles"),
				unitCircleFact(fmt.Sprintf("uc:ref-rad:%d", angle.Degrees), fmt.Sprintf("reference angle for %s", angle.Radians), refRadians, family, "unit_circle_radian", "=", refExplanation, "reference-angles", "radian-degree-conversion"),
				unitCircleFact(fmt.Sprintf("uc:triangle:%d", angle.Degrees), fmt.Sprintf("reference triangle for %s", angle.Radians), triangle, family, "unit_circle_triangle", "=", fmt.Sprintf("%s uses the %s special triangle.", angle.Radians, triangle), "reference-angles", "special-triangle-ratios"),
				unitCircleFact(fmt.Sprintf("uc:ref-sign-sin:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. Is sin positive or negative?", angle.Radians, refRadians, quadrant), signWordForUnitCircleValue(angle.Sin), family, "unit_circle_sign", "sin", "Sine is y, so its sign follows the y-coordinate in the quadrant.", "reference-angles", "quadrant-signs", "sin-is-y"),
				unitCircleFact(fmt.Sprintf("uc:ref-sign-cos:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. Is cos positive or negative?", angle.Radians, refRadians, quadrant), signWordForUnitCircleValue(angle.Cos), family, "unit_circle_sign", "cos", "Cosine is x, so its sign follows the x-coordinate in the quadrant.", "reference-angles", "quadrant-signs", "cos-is-x"),
				unitCircleFact(fmt.Sprintf("uc:ref-sign-tan:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. Is tan positive or negative?", angle.Radians, refRadians, quadrant), signWordForUnitCircleValue(angle.Tan), family, "unit_circle_sign", "tan", "Tangent is y/x, so its sign follows the quotient of the sine and cosine signs.", "reference-angles", "quadrant-signs", "tan-is-y-over-x"),
				unitCircleFact(fmt.Sprintf("uc:ref-value-sin:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. What is sin?", angle.Radians, refRadians, quadrant), angle.Sin, family, "unit_circle_value", "sin", sinExplanation, "reference-angles", "quadrant-signs", "sin-is-y"),
				unitCircleFact(fmt.Sprintf("uc:ref-value-cos:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. What is cos?", angle.Radians, refRadians, quadrant), angle.Cos, family, "unit_circle_value", "cos", cosExplanation, "reference-angles", "quadrant-signs", "cos-is-x"),
				unitCircleFact(fmt.Sprintf("uc:ref-value-tan:%d", angle.Degrees), fmt.Sprintf("%s has reference angle %s and is in quadrant %s. What is tan?", angle.Radians, refRadians, quadrant), angle.Tan, family, "unit_circle_value", "tan", tanExplanation, "reference-angles", "quadrant-signs", "tan-is-y-over-x"),
			)
		}
	}

	for _, degrees := range []int{0, 30, 45, 60, 90} {
		complement := 90 - degrees
		family := fmt.Sprintf("cofunction:%d:%d", degrees, complement)
		explanation := fmt.Sprintf("Sine and cosine swap across complementary angles: sin(theta) = cos(90 deg - theta), so %d deg pairs with %d deg.", degrees, complement)
		facts = append(facts,
			unitCircleFact(fmt.Sprintf("uc:cof:sin-cos:%d", degrees), fmt.Sprintf("sin(%d deg) = cos(? deg)", degrees), fmt.Sprintf("%d deg", complement), family, "unit_circle_degree", "=", explanation, "cofunction-relationships", "sin-is-y", "cos-is-x"),
			unitCircleFact(fmt.Sprintf("uc:cof:cos-sin:%d", degrees), fmt.Sprintf("cos(%d deg) = sin(? deg)", degrees), fmt.Sprintf("%d deg", complement), family, "unit_circle_degree", "=", explanation, "cofunction-relationships", "sin-is-y", "cos-is-x"),
		)
	}

	quadrantSigns := []struct {
		Quadrant string
		Sin      string
		Cos      string
		Tan      string
	}{
		{Quadrant: "I", Sin: "+", Cos: "+", Tan: "+"},
		{Quadrant: "II", Sin: "+", Cos: "-", Tan: "-"},
		{Quadrant: "III", Sin: "-", Cos: "-", Tan: "+"},
		{Quadrant: "IV", Sin: "-", Cos: "+", Tan: "-"},
	}
	for _, signs := range quadrantSigns {
		for _, trig := range []struct {
			Name string
			Sign string
		}{
			{Name: "sin", Sign: signs.Sin},
			{Name: "cos", Sign: signs.Cos},
			{Name: "tan", Sign: signs.Tan},
		} {
			facts = append(facts, Fact{
				ID:               fmt.Sprintf("uc:sign:%s:%s", trig.Name, strings.ToLower(signs.Quadrant)),
				Prompt:           fmt.Sprintf("%s sign in quadrant %s", trig.Name, signs.Quadrant),
				Answer:           trig.Sign,
				Explanation:      unitCircleSignExplanation(trig.Name),
				RelationshipTags: []string{"quadrant-signs", unitCircleFunctionTag(trig.Name)},
				Family:           fmt.Sprintf("signs:%s", signs.Quadrant),
				Kind:             "unit_circle_sign",
				Operator:         trig.Name,
			})
		}
	}

	facts = append(facts, unitCircleConceptFacts()...)
	facts = append(facts, unitCircleReciprocalFacts(angles)...)
	facts = append(facts, unitCircleAssembleFacts(angles)...)
	facts = append(facts, unitCircleReciprocalAngleFacts(angles)...)
	return facts
}

func BuildUnitCircleLessonFacts(lesson UnitCircleLesson) []Fact {
	angles := unitCircleAngles()
	switch lesson {
	case UnitCircleLessonConcepts:
		return filterFacts(unitCircleConceptFacts(), func(fact Fact) bool {
			return !hasRelationshipTag(fact, "reciprocal-functions")
		})
	case UnitCircleLessonQuadrants:
		return filterFacts(BuildUnitCircleFacts(), func(fact Fact) bool {
			return strings.HasPrefix(fact.ID, "uc:quadrant:") || strings.HasPrefix(fact.ID, "uc:sign:")
		})
	case UnitCircleLessonReferenceAngles:
		return filterFacts(BuildUnitCircleFacts(), func(fact Fact) bool {
			return strings.HasPrefix(fact.ID, "uc:ref:") ||
				strings.HasPrefix(fact.ID, "uc:ref-rad:") ||
				strings.HasPrefix(fact.ID, "uc:quadrant:")
		})
	case UnitCircleLessonReferenceValues:
		return filterFacts(BuildUnitCircleFacts(), func(fact Fact) bool {
			if fact.Kind != "unit_circle_value" || fact.Operator == "sec" || fact.Operator == "csc" || fact.Operator == "cot" {
				return false
			}
			return endsWithAny(fact.ID, []string{":30", ":45", ":60"}) &&
				(strings.HasPrefix(fact.ID, "uc:sin:") ||
					strings.HasPrefix(fact.ID, "uc:cos:") ||
					strings.HasPrefix(fact.ID, "uc:tan:"))
		})
	case UnitCircleLessonRadians:
		return filterFacts(BuildUnitCircleFacts(), func(fact Fact) bool {
			return strings.HasPrefix(fact.ID, "uc:deg2rad:") || strings.HasPrefix(fact.ID, "uc:rad2deg:")
		})
	case UnitCircleLessonAssemble:
		return unitCircleAssembleFacts(angles)
	case UnitCircleLessonTangent:
		return filterFacts(BuildUnitCircleFacts(), func(fact Fact) bool {
			return fact.Operator == "tan" &&
				(strings.HasPrefix(fact.ID, "uc:concept:tan-") ||
					strings.HasPrefix(fact.ID, "uc:sign:tan:") ||
					strings.HasPrefix(fact.ID, "uc:tan:"))
		})
	case UnitCircleLessonReciprocals:
		var facts []Fact
		for _, fact := range unitCircleConceptFacts() {
			if hasRelationshipTag(fact, "reciprocal-functions") {
				facts = append(facts, fact)
			}
		}
		facts = append(facts, unitCircleReciprocalFacts(angles)...)
		facts = append(facts, unitCircleReciprocalAngleFacts(angles)...)
		return uniqueFacts(facts)
	case UnitCircleLessonMixed:
		return BuildUnitCircleFacts()
	default:
		return unitCircleConceptFacts()
	}
}

func unitCircleFact(id, prompt, answer, family, kind, operator, explanation string, tags ...string) Fact {
	return Fact{
		ID:               id,
		Prompt:           prompt,
		Answer:           answer,
		Explanation:      explanation,
		RelationshipTags: tags,
		Family:           family,
		Kind:             kind,
		Operator:         operator,
	}
}

func filterFacts(facts []Fact, keep func(Fact) bool) []Fact {
	var out []Fact
	for _, fact := range facts {
		if keep(fact) {
			out = append(out, fact)
		}
	}
	return uniqueFacts(out)
}

func hasRelationshipTag(fact Fact, tag string) bool {
	for _, got := range fact.RelationshipTags {
		if got == tag {
			return true
		}
	}
	return false
}

func endsWithAny(value string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func unitCircleConceptFacts() []Fact {
	return []Fact{
		unitCircleFact("uc:concept:point-order", "Unit-circle point order is (?, ?)", "(cos,sin)", "unit-circle-concepts", "unit_circle_point_order", "=", "The core unit-circle relationship is point = (cos theta, sin theta).", "angle-to-coordinate", "sin-is-y", "cos-is-x"),
		unitCircleFact("uc:concept:sin-y", "On the unit circle, sine is which coordinate?", "y", "unit-circle-concepts", "unit_circle_axis", "sin", "Unit-circle point = (cos theta, sin theta), so sine is y.", "sin-is-y", "angle-to-coordinate"),
		unitCircleFact("uc:concept:cos-x", "On the unit circle, cosine is which coordinate?", "x", "unit-circle-concepts", "unit_circle_axis", "cos", "Unit-circle point = (cos theta, sin theta), so cosine is x.", "cos-is-x", "angle-to-coordinate"),
		unitCircleFact("uc:concept:tan-ratio", "On the unit circle, tangent is what ratio?", "y/x", "unit-circle-concepts", "unit_circle_ratio", "tan", "Tangent is sine divided by cosine, so on the unit circle tan(theta) = y/x.", "tan-is-y-over-x", "sin-is-y", "cos-is-x"),
		unitCircleFact("uc:concept:cot-recip", "cot(theta) is the reciprocal of which function?", "tan", "unit-circle-concepts", "unit_circle_function", "cot", "cot(theta) = 1/tan(theta).", "reciprocal-functions"),
		unitCircleFact("uc:concept:sec-recip", "sec(theta) is the reciprocal of which function?", "cos", "unit-circle-concepts", "unit_circle_function", "sec", "sec(theta) = 1/cos(theta).", "reciprocal-functions", "cos-is-x"),
		unitCircleFact("uc:concept:csc-recip", "csc(theta) is the reciprocal of which function?", "sin", "unit-circle-concepts", "unit_circle_function", "csc", "csc(theta) = 1/sin(theta).", "reciprocal-functions", "sin-is-y"),
	}
}

func unitCircleReciprocalFacts(angles []UnitCircleAngle) []Fact {
	var facts []Fact
	seen := map[string]bool{}
	for _, angle := range angles {
		for _, rel := range []struct {
			Function   string
			Reciprocal string
			Value      string
			Tags       []string
		}{
			{Function: "sin", Reciprocal: "csc", Value: angle.Sin, Tags: []string{"reciprocal-functions", "sin-is-y"}},
			{Function: "cos", Reciprocal: "sec", Value: angle.Cos, Tags: []string{"reciprocal-functions", "cos-is-x"}},
			{Function: "tan", Reciprocal: "cot", Value: angle.Tan, Tags: []string{"reciprocal-functions", "tan-is-y-over-x"}},
		} {
			reciprocal, ok := reciprocalUnitCircleValue(rel.Value)
			if !ok {
				continue
			}
			canonical, _ := normalizeUnitCircleValue(rel.Value)
			if canonical == "undefined" {
				continue
			}
			key := rel.Reciprocal + ":" + canonical
			if seen[key] {
				continue
			}
			seen[key] = true
			explanation := fmt.Sprintf("%s(theta) = 1/%s(theta), so it is the reciprocal of %s.", rel.Reciprocal, rel.Function, rel.Value)
			facts = append(facts, unitCircleFact(
				fmt.Sprintf("uc:reciprocal:%s:%s", rel.Reciprocal, unitCircleValueID(canonical)),
				fmt.Sprintf("If %s(theta) = %s, what is %s(theta)?", rel.Function, rel.Value, rel.Reciprocal),
				reciprocal,
				"unit-circle-reciprocals",
				"unit_circle_value",
				rel.Reciprocal,
				explanation,
				rel.Tags...,
			))
		}
	}
	return facts
}

func unitCircleAssembleFacts(angles []UnitCircleAngle) []Fact {
	var facts []Fact
	for _, angle := range angles {
		quadrant, ok := quadrantForDegrees(angle.Degrees)
		if !ok {
			continue
		}
		refRadians := radiansForDegrees(referenceAngle(angle.Degrees))
		family := fmt.Sprintf("assemble:%s:%s", refRadians, quadrant)
		facts = append(facts,
			unitCircleFact(
				fmt.Sprintf("uc:assemble:sin:%d", angle.Degrees),
				fmt.Sprintf("sin at reference angle %s in quadrant %s", refRadians, quadrant),
				angle.Sin,
				family,
				"unit_circle_value",
				"sin",
				"Sine uses the reference-angle y-value, then applies the quadrant sign.",
				"reference-angles",
				"quadrant-signs",
				"sin-is-y",
			),
			unitCircleFact(
				fmt.Sprintf("uc:assemble:cos:%d", angle.Degrees),
				fmt.Sprintf("cos at reference angle %s in quadrant %s", refRadians, quadrant),
				angle.Cos,
				family,
				"unit_circle_value",
				"cos",
				"Cosine uses the reference-angle x-value, then applies the quadrant sign.",
				"reference-angles",
				"quadrant-signs",
				"cos-is-x",
			),
		)
	}
	return facts
}

func unitCircleReciprocalAngleFacts(angles []UnitCircleAngle) []Fact {
	var facts []Fact
	for _, angle := range angles {
		for _, rel := range []struct {
			Function   string
			Reciprocal string
			Value      string
			Tags       []string
		}{
			{Function: "sin", Reciprocal: "csc", Value: angle.Sin, Tags: []string{"reciprocal-functions", "sin-is-y"}},
			{Function: "cos", Reciprocal: "sec", Value: angle.Cos, Tags: []string{"reciprocal-functions", "cos-is-x"}},
			{Function: "tan", Reciprocal: "cot", Value: angle.Tan, Tags: []string{"reciprocal-functions", "tan-is-y-over-x"}},
		} {
			reciprocal, ok := reciprocalUnitCircleValue(rel.Value)
			if !ok {
				continue
			}
			explanation := fmt.Sprintf("%s(theta) = 1/%s(theta), so use the reciprocal of %s.", rel.Reciprocal, rel.Function, rel.Value)
			facts = append(facts,
				unitCircleFact(
					fmt.Sprintf("uc:reciprocal-angle:%s:deg:%d", rel.Reciprocal, angle.Degrees),
					fmt.Sprintf("%s(%d deg)", rel.Reciprocal, angle.Degrees),
					reciprocal,
					fmt.Sprintf("reciprocal-angle:%d", angle.Degrees),
					"unit_circle_value",
					rel.Reciprocal,
					explanation,
					rel.Tags...,
				),
				unitCircleFact(
					fmt.Sprintf("uc:reciprocal-angle:%s:rad:%d", rel.Reciprocal, angle.Degrees),
					fmt.Sprintf("%s(%s)", rel.Reciprocal, angle.Radians),
					reciprocal,
					fmt.Sprintf("reciprocal-angle:%d", angle.Degrees),
					"unit_circle_value",
					rel.Reciprocal,
					explanation,
					rel.Tags...,
				),
			)
		}
	}
	return facts
}

func unitCircleAngles() []UnitCircleAngle {
	return []UnitCircleAngle{
		{Degrees: 0, Radians: "0", Sin: "0", Cos: "1", Tan: "0"},
		{Degrees: 30, Radians: "pi/6", Sin: "1/2", Cos: "sqrt(3)/2", Tan: "sqrt(3)/3"},
		{Degrees: 45, Radians: "pi/4", Sin: "sqrt(2)/2", Cos: "sqrt(2)/2", Tan: "1"},
		{Degrees: 60, Radians: "pi/3", Sin: "sqrt(3)/2", Cos: "1/2", Tan: "sqrt(3)"},
		{Degrees: 90, Radians: "pi/2", Sin: "1", Cos: "0", Tan: "undefined"},
		{Degrees: 120, Radians: "2pi/3", Sin: "sqrt(3)/2", Cos: "-1/2", Tan: "-sqrt(3)"},
		{Degrees: 135, Radians: "3pi/4", Sin: "sqrt(2)/2", Cos: "-sqrt(2)/2", Tan: "-1"},
		{Degrees: 150, Radians: "5pi/6", Sin: "1/2", Cos: "-sqrt(3)/2", Tan: "-sqrt(3)/3"},
		{Degrees: 180, Radians: "pi", Sin: "0", Cos: "-1", Tan: "0"},
		{Degrees: 210, Radians: "7pi/6", Sin: "-1/2", Cos: "-sqrt(3)/2", Tan: "sqrt(3)/3"},
		{Degrees: 225, Radians: "5pi/4", Sin: "-sqrt(2)/2", Cos: "-sqrt(2)/2", Tan: "1"},
		{Degrees: 240, Radians: "4pi/3", Sin: "-sqrt(3)/2", Cos: "-1/2", Tan: "sqrt(3)"},
		{Degrees: 270, Radians: "3pi/2", Sin: "-1", Cos: "0", Tan: "undefined"},
		{Degrees: 300, Radians: "5pi/3", Sin: "-sqrt(3)/2", Cos: "1/2", Tan: "-sqrt(3)"},
		{Degrees: 315, Radians: "7pi/4", Sin: "-sqrt(2)/2", Cos: "sqrt(2)/2", Tan: "-1"},
		{Degrees: 330, Radians: "11pi/6", Sin: "-1/2", Cos: "sqrt(3)/2", Tan: "-sqrt(3)/3"},
		{Degrees: 360, Radians: "2pi", Sin: "0", Cos: "1", Tan: "0"},
	}
}

func quadrantForDegrees(degrees int) (string, bool) {
	degrees %= 360
	switch {
	case degrees > 0 && degrees < 90:
		return "I", true
	case degrees > 90 && degrees < 180:
		return "II", true
	case degrees > 180 && degrees < 270:
		return "III", true
	case degrees > 270 && degrees < 360:
		return "IV", true
	default:
		return "", false
	}
}

func referenceAngle(degrees int) int {
	degrees %= 360
	switch {
	case degrees <= 90:
		return degrees
	case degrees <= 180:
		return 180 - degrees
	case degrees <= 270:
		return degrees - 180
	default:
		return 360 - degrees
	}
}

func radiansForDegrees(degrees int) string {
	degrees %= 360
	for _, angle := range unitCircleAngles() {
		if angle.Degrees%360 == degrees {
			return angle.Radians
		}
	}
	return fmt.Sprintf("%d deg", degrees)
}

func referenceTriangle(degrees int) string {
	switch degrees {
	case 30, 60:
		return "30-60-90"
	case 45:
		return "45-45-90"
	default:
		return "axis"
	}
}

func signWordForUnitCircleValue(value string) string {
	canonical, ok := normalizeUnitCircleValue(value)
	if !ok || canonical == "undefined" {
		return "undefined"
	}
	if canonical == "0" {
		return "zero"
	}
	if strings.HasPrefix(canonical, "-") {
		return "negative"
	}
	return "positive"
}

func unitCircleSignExplanation(function string) string {
	switch function {
	case "sin":
		return "Sine is y, so its sign follows the y-coordinate in that quadrant."
	case "cos":
		return "Cosine is x, so its sign follows the x-coordinate in that quadrant."
	case "tan":
		return "Tangent is y/x, so its sign follows the quotient of the sine and cosine signs."
	default:
		return "The sign comes from the unit-circle coordinate relationship."
	}
}

func unitCircleFunctionTag(function string) string {
	switch function {
	case "sin":
		return "sin-is-y"
	case "cos":
		return "cos-is-x"
	case "tan":
		return "tan-is-y-over-x"
	default:
		return "unit-circle-functions"
	}
}

func reciprocalUnitCircleValue(value string) (string, bool) {
	canonical, ok := normalizeUnitCircleValue(value)
	if !ok {
		return "", false
	}
	reciprocals := map[string]string{
		"0":         "undefined",
		"undefined": "0",
		"1":         "1",
		"-1":        "-1",
		"1/2":       "2",
		"-1/2":      "-2",
		"sqrt2/2":   "sqrt(2)",
		"-sqrt2/2":  "-sqrt(2)",
		"sqrt2":     "sqrt(2)/2",
		"-sqrt2":    "-sqrt(2)/2",
		"sqrt3/2":   "2sqrt(3)/3",
		"-sqrt3/2":  "-2sqrt(3)/3",
		"2sqrt3/3":  "sqrt(3)/2",
		"-2sqrt3/3": "-sqrt(3)/2",
		"sqrt3":     "sqrt(3)/3",
		"-sqrt3":    "-sqrt(3)/3",
		"sqrt3/3":   "sqrt(3)",
		"-sqrt3/3":  "-sqrt(3)",
	}
	reciprocal, ok := reciprocals[canonical]
	return reciprocal, ok
}

func unitCircleValueID(value string) string {
	replacer := strings.NewReplacer(
		"-", "neg",
		"/", "over",
		"(", "",
		")", "",
		"sqrt", "sqrt",
	)
	return replacer.Replace(value)
}

func BuildExponentLogFacts() []Fact {
	bases := []int{2, 3, 4, 5, 10}
	exponents := []int{-3, -2, -1, 0, 1, 2, 3, 4, 5}
	var facts []Fact
	for _, base := range bases {
		for _, exponent := range exponents {
			value := powerValue(base, exponent)
			exponentText := strconv.Itoa(exponent)
			magnitude := intPow(base, abs(exponent))
			exponentExpression := fmt.Sprintf("%d^%d=%s", base, exponent, value)
			logExpression := fmt.Sprintf("log_%d(%s)=%d", base, value, exponent)
			family := fmt.Sprintf("%s <=> %s", exponentExpression, logExpression)
			rootExpression := ""
			if exponent > 1 {
				rootExpression = fmt.Sprintf("root_%d(%s)=%d", exponent, value, base)
				family = fmt.Sprintf("%s <=> %s <=> %s", exponentExpression, logExpression, rootExpression)
			}
			facts = append(facts,
				Fact{
					ID:       fmt.Sprintf("exlog:pow:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("%d^%d", base, exponent),
					Answer:   value,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "^",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:log:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("log_%d(%s)", base, value),
					Answer:   exponentText,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "log",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:pow2log:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("%s as log", exponentExpression),
					Answer:   logExpression,
					Family:   family,
					Kind:     "exponent_log_form",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "log",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:log2pow:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("%s as exponent", logExpression),
					Answer:   exponentExpression,
					Family:   family,
					Kind:     "log_exponent_form",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "^",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:missing-exp:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("%d^? = %s", base, value),
					Answer:   exponentText,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "^",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:missing-value:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("log_%d(?) = %d", base, exponent),
					Answer:   value,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "log",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:inverse-log-pow:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("log_%d(%d^%d)", base, base, exponent),
					Answer:   exponentText,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "log",
				},
				Fact{
					ID:       fmt.Sprintf("exlog:inverse-pow-log:%d:%d", base, exponent),
					Prompt:   fmt.Sprintf("%d^(log_%d(%s))", base, base, value),
					Answer:   value,
					Family:   family,
					Kind:     "exponent_log_number",
					A:        base,
					B:        exponent,
					Product:  magnitude,
					Operator: "^",
				},
			)
			if exponent > 1 {
				explanation := fmt.Sprintf("The same relationship has three forms: %s, %s, and %s. The root form asks which base raised to %d gives %s.", exponentExpression, logExpression, rootExpression, exponent, value)
				facts = append(facts,
					Fact{
						ID:               fmt.Sprintf("exlog:pow2root:%d:%d", base, exponent),
						Prompt:           fmt.Sprintf("%s as root", exponentExpression),
						Answer:           rootExpression,
						Explanation:      explanation,
						RelationshipTags: []string{"base-exponent-value", "root-exponent-log-equivalence", "exponential-form-to-root-form", "source-exponent", "target-root"},
						Family:           family,
						Kind:             "root_form",
						A:                base,
						B:                exponent,
						Product:          magnitude,
						Operator:         "root",
					},
					Fact{
						ID:               fmt.Sprintf("exlog:log2root:%d:%d", base, exponent),
						Prompt:           fmt.Sprintf("%s as root", logExpression),
						Answer:           rootExpression,
						Explanation:      explanation,
						RelationshipTags: []string{"base-exponent-value", "root-exponent-log-equivalence", "logarithmic-form-to-root-form", "source-log", "target-root"},
						Family:           family,
						Kind:             "root_form",
						A:                base,
						B:                exponent,
						Product:          magnitude,
						Operator:         "root",
					},
					Fact{
						ID:               fmt.Sprintf("exlog:root2pow:%d:%d", base, exponent),
						Prompt:           fmt.Sprintf("%s as exponent", rootExpression),
						Answer:           exponentExpression,
						Explanation:      explanation,
						RelationshipTags: []string{"base-exponent-value", "root-exponent-log-equivalence", "root-form-to-exponential-form", "source-root", "target-exponent"},
						Family:           family,
						Kind:             "log_exponent_form",
						A:                base,
						B:                exponent,
						Product:          magnitude,
						Operator:         "^",
					},
					Fact{
						ID:               fmt.Sprintf("exlog:root2log:%d:%d", base, exponent),
						Prompt:           fmt.Sprintf("%s as log", rootExpression),
						Answer:           logExpression,
						Explanation:      explanation,
						RelationshipTags: []string{"base-exponent-value", "root-exponent-log-equivalence", "root-form-to-logarithmic-form", "source-root", "target-log"},
						Family:           family,
						Kind:             "exponent_log_form",
						A:                base,
						B:                exponent,
						Product:          magnitude,
						Operator:         "log",
					},
				)
			}
			if exponent != 0 {
				facts = append(facts,
					Fact{
						ID:       fmt.Sprintf("exlog:missing-base-pow:%d:%d", base, exponent),
						Prompt:   fmt.Sprintf("?^%d = %s", exponent, value),
						Answer:   strconv.Itoa(base),
						Family:   family,
						Kind:     "exponent_log_number",
						A:        base,
						B:        exponent,
						Product:  magnitude,
						Operator: "^",
					},
					Fact{
						ID:       fmt.Sprintf("exlog:missing-base-log:%d:%d", base, exponent),
						Prompt:   fmt.Sprintf("log_?(%s) = %d", value, exponent),
						Answer:   strconv.Itoa(base),
						Family:   family,
						Kind:     "exponent_log_number",
						A:        base,
						B:        exponent,
						Product:  magnitude,
						Operator: "log",
					},
				)
			}
		}
	}
	facts = append(facts, exponentLogConceptFacts()...)
	return annotateExponentLogFacts(facts)
}

func annotateExponentLogFacts(facts []Fact) []Fact {
	for i := range facts {
		if !strings.HasPrefix(facts[i].ID, "exlog:") {
			continue
		}
		if facts[i].Explanation == "" {
			base := facts[i].A
			exponent := facts[i].B
			value := powerValue(base, exponent)
			facts[i].Explanation = fmt.Sprintf("The same relationship has two forms: %d^%d = %s and log_%d(%s) = %d. A logarithm asks for the exponent.", base, exponent, value, base, value, exponent)
		}
		if len(facts[i].RelationshipTags) == 0 {
			tags := []string{"base-exponent-value", "exponential-logarithmic-equivalence", "inverse-operations"}
			if facts[i].B < 0 {
				tags = append(tags, "negative-exponents-are-reciprocals")
			}
			switch {
			case strings.Contains(facts[i].ID, "missing-base"):
				tags = append(tags, "missing-base")
			case strings.Contains(facts[i].ID, "missing-exp"):
				tags = append(tags, "missing-exponent")
			case strings.Contains(facts[i].ID, "missing-value"):
				tags = append(tags, "missing-value")
			case strings.Contains(facts[i].ID, "pow2log"):
				tags = append(tags, "exponential-form-to-logarithmic-form")
			case strings.Contains(facts[i].ID, "log2pow"):
				tags = append(tags, "logarithmic-form-to-exponential-form")
			}
			facts[i].RelationshipTags = tags
		}
	}
	return facts
}

func exponentLogConceptFacts() []Fact {
	return []Fact{
		{
			ID:               "exlog:concept:log-asks-for",
			Prompt:           "In log_b(y) = x, what is the log asking for?",
			Answer:           "exponent",
			Explanation:      "A logarithm asks which exponent on the base produces the value.",
			RelationshipTags: []string{"base-exponent-value", "log-asks-for-exponent"},
			Family:           "exponent-log-concepts",
			Kind:             "exponent_log_role",
			Operator:         "log",
		},
		{
			ID:               "exlog:concept:base",
			Prompt:           "In b^x = y, which part is the repeated multiplier?",
			Answer:           "base",
			Explanation:      "The base is the repeated multiplier; the exponent says how many times the base is used.",
			RelationshipTags: []string{"base-exponent-value"},
			Family:           "exponent-log-concepts",
			Kind:             "exponent_log_role",
			Operator:         "^",
		},
		{
			ID:               "exlog:concept:zero-exponent",
			Prompt:           "For nonzero b, b^0",
			Answer:           "1",
			Explanation:      "A zero exponent leaves no factors of b, so the multiplicative identity 1 remains.",
			RelationshipTags: []string{"zero-exponent", "multiplicative-identity"},
			Family:           "exponent-log-concepts",
			Kind:             "exact_quantity",
			Operator:         "^",
		},
		{
			ID:               "exlog:concept:log-base",
			Prompt:           "For b > 0, log_b(b)",
			Answer:           "1",
			Explanation:      "log_b(b) asks which exponent on b gives b; b^1 = b.",
			RelationshipTags: []string{"base-exponent-value", "log-asks-for-exponent"},
			Family:           "exponent-log-concepts",
			Kind:             "exact_quantity",
			Operator:         "log",
		},
		{
			ID:               "exlog:concept:log-one",
			Prompt:           "For b > 0, log_b(1)",
			Answer:           "0",
			Explanation:      "log_b(1) asks which exponent on b gives 1; b^0 = 1.",
			RelationshipTags: []string{"zero-exponent", "log-asks-for-exponent"},
			Family:           "exponent-log-concepts",
			Kind:             "exact_quantity",
			Operator:         "log",
		},
	}
}

func powerValue(base, exponent int) string {
	power := intPow(base, abs(exponent))
	if exponent < 0 {
		return fmt.Sprintf("1/%d", power)
	}
	return strconv.Itoa(power)
}

func intPow(base, exponent int) int {
	result := 1
	for i := 0; i < exponent; i++ {
		result *= base
	}
	return result
}

type algebraIdentitySpec struct {
	ID          string
	Prompt      string
	Answer      string
	Family      string
	Explanation string
	Tags        []string
	Kind        string
	Operator    string
}

func BuildAlgebraIdentityFacts() []Fact {
	var facts []Fact
	facts = append(facts, algebraIdentityConceptFacts()...)
	facts = append(facts, algebraIdentityExpandFacts()...)
	facts = append(facts, algebraIdentityFactorFacts()...)
	facts = append(facts, algebraIdentityRecognizeFacts()...)
	facts = append(facts, algebraIdentityMixedExtraFacts()...)
	return uniqueFacts(facts)
}

func BuildAlgebraIdentityLessonFacts(lesson AlgebraIdentityLesson) []Fact {
	switch lesson {
	case "", AlgebraIdentityLessonConcepts:
		return uniqueFacts(algebraIdentityConceptFacts())
	case AlgebraIdentityLessonExpand:
		return uniqueFacts(algebraIdentityExpandFacts())
	case AlgebraIdentityLessonFactor:
		return uniqueFacts(algebraIdentityFactorFacts())
	case AlgebraIdentityLessonRecognize:
		return uniqueFacts(algebraIdentityRecognizeFacts())
	case AlgebraIdentityLessonMixed:
		return BuildAlgebraIdentityFacts()
	default:
		log.Fatalf("unknown algebra-identities lesson %q; use concepts, expand, factor, recognize, or mixed", lesson)
		return algebraIdentityConceptFacts()
	}
}

func algebraIdentityConceptFacts() []Fact {
	return []Fact{
		{
			ID:               "algid:concept:identity",
			Prompt:           "What does identity mean?",
			Answer:           "true for all allowed values",
			Explanation:      "An identity is a statement that remains true for every value where both sides are defined.",
			RelationshipTags: algebraIdentityTags([]string{"algebra-vocabulary", "identity", "equivalent-expressions"}),
			Family:           "algebra-identity-concepts",
			Kind:             "algebra_identity_concept",
			Operator:         "=",
		},
		{
			ID:               "algid:concept:expand",
			Prompt:           "What does expand mean?",
			Answer:           "write as a sum of terms",
			Explanation:      "Expanding rewrites a product or power by distributing multiplication into separate terms.",
			RelationshipTags: algebraIdentityTags([]string{"algebra-vocabulary", "expand", "equivalent-expressions"}),
			Family:           "algebra-identity-concepts",
			Kind:             "algebra_identity_concept",
			Operator:         "=",
		},
		{
			ID:               "algid:concept:factor",
			Prompt:           "What does factor mean?",
			Answer:           "write as a product of factors",
			Explanation:      "Factoring rewrites a sum or difference as multiplication of shared or patterned factors.",
			RelationshipTags: algebraIdentityTags([]string{"algebra-vocabulary", "factor", "equivalent-expressions"}),
			Family:           "algebra-identity-concepts",
			Kind:             "algebra_identity_concept",
			Operator:         "=",
		},
		{
			ID:               "algid:concept:equivalent-expression",
			Prompt:           "What does equivalent expression mean?",
			Answer:           "same value for the same inputs",
			Explanation:      "Equivalent expressions may look different, but they produce the same value for the same allowed inputs.",
			RelationshipTags: algebraIdentityTags([]string{"algebra-vocabulary", "equivalent-expressions"}),
			Family:           "algebra-identity-concepts",
			Kind:             "algebra_identity_concept",
			Operator:         "=",
		},
		{
			ID:               "algid:concept:distribute",
			Prompt:           "What does distribute mean?",
			Answer:           "multiply into each term",
			Explanation:      "Distributing applies multiplication to each term inside parentheses.",
			RelationshipTags: algebraIdentityTags([]string{"algebra-vocabulary", "distribute", "distributive-property", "expand"}),
			Family:           "algebra-identity-concepts",
			Kind:             "algebra_identity_concept",
			Operator:         "=",
		},
	}
}

func algebraIdentityExpandFacts() []Fact {
	return algebraIdentityFactsFromSpecs([]algebraIdentitySpec{
		{
			ID:          "algid:expand:square-sum",
			Prompt:      "expand (a+b)^2",
			Answer:      "a^2+2ab+b^2",
			Family:      "(a+b)^2=a^2+2ab+b^2",
			Explanation: "Squaring a sum makes two square terms and the middle double-product term.",
			Tags:        []string{"square-of-sum", "perfect-square-trinomial", "expand", "binomial-patterns"},
		},
		{
			ID:          "algid:expand:square-difference",
			Prompt:      "expand (a-b)^2",
			Answer:      "a^2-2ab+b^2",
			Family:      "(a-b)^2=a^2-2ab+b^2",
			Explanation: "Squaring a difference keeps both square terms positive and makes the middle double-product term negative.",
			Tags:        []string{"square-of-difference", "perfect-square-trinomial", "expand", "binomial-patterns"},
		},
		{
			ID:          "algid:expand:difference-squares",
			Prompt:      "expand (a+b)(a-b)",
			Answer:      "a^2-b^2",
			Family:      "(a+b)(a-b)=a^2-b^2",
			Explanation: "Conjugates cancel the middle terms, leaving a difference of squares.",
			Tags:        []string{"difference-of-squares", "expand", "conjugates"},
		},
		{
			ID:          "algid:expand:x-times-sum",
			Prompt:      "expand x(x+a)",
			Answer:      "x^2+ax",
			Family:      "x(x+a)=x^2+ax",
			Explanation: "Distribute x into both terms inside the parentheses.",
			Tags:        []string{"distributive-property", "expand", "factoring-common-factor"},
		},
		{
			ID:          "algid:expand:distributive",
			Prompt:      "expand a(b+c)",
			Answer:      "ab+ac",
			Family:      "a(b+c)=ab+ac",
			Explanation: "Distribution multiplies the outside factor into each term inside the parentheses.",
			Tags:        []string{"distributive-property", "expand", "factoring-common-factor"},
		},
		{
			ID:          "algid:expand:distributive-difference",
			Prompt:      "expand a(b-c)",
			Answer:      "ab-ac",
			Family:      "a(b-c)=ab-ac",
			Explanation: "Distribution multiplies a into b and into -c, producing ab - ac.",
			Tags:        []string{"distributive-property", "expand", "factoring-common-factor"},
		},
	})
}

func algebraIdentityFactorFacts() []Fact {
	return algebraIdentityFactsFromSpecs([]algebraIdentitySpec{
		{
			ID:          "algid:factor:square-sum",
			Prompt:      "factor a^2+2ab+b^2",
			Answer:      "(a+b)^2",
			Family:      "(a+b)^2=a^2+2ab+b^2",
			Explanation: "A perfect-square trinomial has matching square terms and a positive middle double-product term.",
			Tags:        []string{"square-of-sum", "perfect-square-trinomial", "factor", "binomial-patterns"},
		},
		{
			ID:          "algid:factor:square-difference",
			Prompt:      "factor a^2-2ab+b^2",
			Answer:      "(a-b)^2",
			Family:      "(a-b)^2=a^2-2ab+b^2",
			Explanation: "A negative middle double-product term identifies the square of a difference.",
			Tags:        []string{"square-of-difference", "perfect-square-trinomial", "factor", "binomial-patterns"},
		},
		{
			ID:          "algid:factor:difference-squares",
			Prompt:      "factor a^2-b^2",
			Answer:      "(a+b)(a-b)",
			Family:      "a^2-b^2=(a+b)(a-b)",
			Explanation: "A difference of squares factors into conjugates.",
			Tags:        []string{"difference-of-squares", "factor", "conjugates"},
		},
		{
			ID:          "algid:factor:x-common-factor",
			Prompt:      "factor x^2+ax",
			Answer:      "x(x+a)",
			Family:      "x(x+a)=x^2+ax",
			Explanation: "Both terms share the factor x, so factor it out.",
			Tags:        []string{"distributive-property", "factor", "factoring-common-factor"},
		},
	})
}

func algebraIdentityRecognizeFacts() []Fact {
	return algebraIdentityFactsFromSpecs([]algebraIdentitySpec{
		{
			ID:          "algid:recognize:x2-minus-9",
			Prompt:      "what pattern does x^2-9 match?",
			Answer:      "difference of squares",
			Family:      "pattern-recognition:difference-of-squares",
			Explanation: "x^2 and 9 are both squares, and the expression subtracts one from the other.",
			Tags:        []string{"recognize-patterns", "difference-of-squares"},
			Kind:        "algebra_pattern_name",
		},
		{
			ID:          "algid:recognize:x2-plus-6x-plus-9",
			Prompt:      "what pattern does x^2+6x+9 match?",
			Answer:      "perfect square trinomial",
			Family:      "pattern-recognition:perfect-square-trinomial",
			Explanation: "x^2 and 9 are square terms, and 6x is the middle double-product term.",
			Tags:        []string{"recognize-patterns", "perfect-square-trinomial"},
			Kind:        "algebra_pattern_name",
		},
		{
			ID:          "algid:recognize:4x2-minus-25",
			Prompt:      "what pattern does 4x^2-25 match?",
			Answer:      "difference of squares",
			Family:      "pattern-recognition:difference-of-squares",
			Explanation: "4x^2 is (2x)^2 and 25 is 5^2, so this is a difference of squares.",
			Tags:        []string{"recognize-patterns", "difference-of-squares"},
			Kind:        "algebra_pattern_name",
		},
	})
}

func algebraIdentityMixedExtraFacts() []Fact {
	return algebraIdentityFactsFromSpecs([]algebraIdentitySpec{
		{
			ID:          "algid:factor:sum-cubes",
			Prompt:      "factor a^3+b^3",
			Answer:      "(a+b)(a^2-ab+b^2)",
			Family:      "a^3+b^3=(a+b)(a^2-ab+b^2)",
			Explanation: "A sum of cubes factors with the same sign in the binomial and alternating signs in the trinomial.",
			Tags:        []string{"sum-of-cubes", "factor", "cubic-patterns"},
		},
		{
			ID:          "algid:factor:difference-cubes",
			Prompt:      "factor a^3-b^3",
			Answer:      "(a-b)(a^2+ab+b^2)",
			Family:      "a^3-b^3=(a-b)(a^2+ab+b^2)",
			Explanation: "A difference of cubes factors with the same sign in the binomial and positive middle term in the trinomial.",
			Tags:        []string{"difference-of-cubes", "factor", "cubic-patterns"},
		},
		{
			ID:          "algid:factor:common-factor-a",
			Prompt:      "factor ab+ac",
			Answer:      "a(b+c)",
			Family:      "a(b+c)=ab+ac",
			Explanation: "Both terms share the factor a, so factor it out.",
			Tags:        []string{"distributive-property", "factor", "factoring-common-factor"},
		},
		{
			ID:          "algid:factor:common-factor-x",
			Prompt:      "factor xy+xz",
			Answer:      "x(y+z)",
			Family:      "x(y+z)=xy+xz",
			Explanation: "Both terms share the factor x, so factor it out.",
			Tags:        []string{"distributive-property", "factor", "factoring-common-factor"},
		},
		{
			ID:          "algid:expand:binomial-product",
			Prompt:      "expand (x+a)(x+b)",
			Answer:      "x^2+(a+b)x+ab",
			Family:      "(x+a)(x+b)=x^2+(a+b)x+ab",
			Explanation: "The middle coefficient is the sum of the added terms; the constant term is their product.",
			Tags:        []string{"binomial-product", "expand", "pattern-transformations"},
		},
		{
			ID:          "algid:factor:binomial-product",
			Prompt:      "factor x^2+(a+b)x+ab",
			Answer:      "(x+a)(x+b)",
			Family:      "(x+a)(x+b)=x^2+(a+b)x+ab",
			Explanation: "This trinomial pattern reverses the product of two binomials with matching x terms.",
			Tags:        []string{"binomial-product", "factor", "pattern-transformations"},
		},
		{
			ID:          "algid:factor:numeric-difference-squares-9",
			Prompt:      "factor x^2-9",
			Answer:      "(x+3)(x-3)",
			Family:      "x^2-9=(x+3)(x-3)",
			Explanation: "x^2 is x squared and 9 is 3 squared, so this matches a^2-b^2=(a+b)(a-b).",
			Tags:        []string{"difference-of-squares", "factor", "numeric-recognizer"},
		},
		{
			ID:          "algid:factor:numeric-difference-squares-16",
			Prompt:      "factor x^2-16",
			Answer:      "(x+4)(x-4)",
			Family:      "x^2-16=(x+4)(x-4)",
			Explanation: "x^2 is x squared and 16 is 4 squared, so this matches a^2-b^2=(a+b)(a-b).",
			Tags:        []string{"difference-of-squares", "factor", "numeric-recognizer"},
		},
		{
			ID:          "algid:factor:numeric-perfect-square-plus",
			Prompt:      "factor x^2+6x+9",
			Answer:      "(x+3)^2",
			Family:      "x^2+6x+9=(x+3)^2",
			Explanation: "x^2 and 9 are square terms, and 6x is 2*x*3, so this is a perfect-square trinomial.",
			Tags:        []string{"perfect-square-trinomial", "factor", "numeric-recognizer"},
		},
		{
			ID:          "algid:factor:numeric-perfect-square-minus",
			Prompt:      "factor x^2-10x+25",
			Answer:      "(x-5)^2",
			Family:      "x^2-10x+25=(x-5)^2",
			Explanation: "x^2 and 25 are square terms, and -10x is -2*x*5, so this is a perfect-square trinomial.",
			Tags:        []string{"perfect-square-trinomial", "factor", "numeric-recognizer"},
		},
	})
}

func algebraIdentityTags(tags []string) []string {
	for _, tag := range tags {
		if tag == "algebra-identity" {
			return tags
		}
	}
	out := make([]string, 0, len(tags)+1)
	out = append(out, "algebra-identity")
	return append(out, tags...)
}

func algebraIdentityFactsFromSpecs(specs []algebraIdentitySpec) []Fact {
	facts := make([]Fact, 0, len(specs))
	for _, spec := range specs {
		kind := spec.Kind
		if kind == "" {
			kind = "algebra_identity"
		}
		operator := spec.Operator
		if operator == "" {
			operator = "="
		}
		facts = append(facts, Fact{
			ID:               spec.ID,
			Prompt:           spec.Prompt,
			Answer:           spec.Answer,
			Explanation:      spec.Explanation,
			RelationshipTags: algebraIdentityTags(spec.Tags),
			Family:           spec.Family,
			Kind:             kind,
			Operator:         operator,
		})
	}
	return facts
}

func BuildTriangleFacts() []Fact {
	facts := []Fact{
		triangleFact(
			"tri:concept:angle-sum",
			"What do the angles in any triangle add up to?",
			"180",
			"triangle-angle-sum",
			"exact_quantity",
			"angle-sum",
			"Every triangle has interior angles that sum to 180 degrees.",
			"triangle-relationships", "angle-sum",
		),
		triangleFact(
			"tri:concept:right-angle",
			"A right triangle has one angle of how many degrees?",
			"90",
			"right-triangle-basics",
			"exact_quantity",
			"right-angle",
			"A right triangle is defined by having exactly one 90-degree angle.",
			"triangle-relationships", "right-triangle",
		),
		triangleFact(
			"tri:concept:acute-complement",
			"In a right triangle, the two acute angles add up to how many degrees?",
			"90",
			"right-triangle-basics",
			"exact_quantity",
			"angle-sum",
			"The full triangle sums to 180 degrees, and the right angle uses 90 degrees, leaving 90 degrees for the two acute angles.",
			"triangle-relationships", "right-triangle", "complementary-angles",
		),
		triangleFact(
			"tri:angle-sum:missing:60:60",
			"A triangle has angles 60 deg and 60 deg. What is the missing angle?",
			"60",
			"triangle-angle-sum",
			"exact_quantity",
			"angle-sum",
			"Triangle angles sum to 180 degrees, so 180 - 60 - 60 = 60.",
			"triangle-relationships", "angle-sum", "missing-angle",
		),
		triangleFact(
			"tri:angle-sum:missing:30:90",
			"A triangle has angles 30 deg and 90 deg. What is the missing angle?",
			"60",
			"triangle-angle-sum",
			"exact_quantity",
			"angle-sum",
			"Triangle angles sum to 180 degrees, so 180 - 30 - 90 = 60.",
			"triangle-relationships", "angle-sum", "missing-angle",
		),
		triangleFact(
			"tri:angle-sum:missing:45:45",
			"A triangle has angles 45 deg and 45 deg. What is the missing angle?",
			"90",
			"triangle-angle-sum",
			"exact_quantity",
			"angle-sum",
			"Triangle angles sum to 180 degrees, so 180 - 45 - 45 = 90.",
			"triangle-relationships", "angle-sum", "missing-angle",
		),
		triangleFact(
			"tri:side:hypotenuse",
			"In a right triangle, what is the side opposite the right angle called?",
			"hypotenuse",
			"right-triangle-side-names",
			"triangle_term",
			"side-name",
			"The hypotenuse is across from the 90-degree angle and is the longest side.",
			"triangle-relationships", "right-triangle", "opposite-adjacent-hypotenuse",
		),
		triangleFact(
			"tri:side:opposite",
			"Relative to an acute angle, what is the side across from that angle called?",
			"opposite",
			"right-triangle-side-names",
			"triangle_term",
			"side-name",
			"The opposite side is across the triangle from the angle you are using.",
			"triangle-relationships", "right-triangle", "opposite-adjacent-hypotenuse",
		),
		triangleFact(
			"tri:side:adjacent",
			"Relative to an acute angle, what is the leg that touches the angle called?",
			"adjacent",
			"right-triangle-side-names",
			"triangle_term",
			"side-name",
			"The adjacent leg touches the angle you are using; the hypotenuse also touches it but is named separately.",
			"triangle-relationships", "right-triangle", "opposite-adjacent-hypotenuse",
		),
		triangleFact(
			"tri:pythagorean:formula",
			"For right triangle legs a and b with hypotenuse c, what equation relates the sides?",
			"a^2+b^2=c^2",
			"pythagorean-theorem",
			"triangle_formula",
			"pythagorean",
			"The squares on the legs add to the square on the hypotenuse.",
			"triangle-relationships", "right-triangle", "pythagorean-theorem",
		),
		triangleFact(
			"tri:pythagorean:3-4-5:hypotenuse",
			"A right triangle has legs 3 and 4. What is the hypotenuse?",
			"5",
			"pythagorean-triples",
			"exact_quantity",
			"pythagorean",
			"3^2 + 4^2 = 9 + 16 = 25, and sqrt(25) = 5.",
			"triangle-relationships", "right-triangle", "pythagorean-theorem",
		),
		triangleFact(
			"tri:pythagorean:5-12-13:leg",
			"A right triangle has hypotenuse 13 and one leg 5. What is the other leg?",
			"12",
			"pythagorean-triples",
			"exact_quantity",
			"pythagorean",
			"13^2 - 5^2 = 169 - 25 = 144, and sqrt(144) = 12.",
			"triangle-relationships", "right-triangle", "pythagorean-theorem",
		),
		triangleFact(
			"tri:special:45-45-90:ratio",
			"What is the leg:leg:hypotenuse ratio in a 45-45-90 triangle?",
			"1:1:sqrt(2)",
			"special-right-triangles",
			"triangle_ratio",
			"ratio",
			"A 45-45-90 triangle is isosceles, so the legs match and the hypotenuse is leg*sqrt(2).",
			"triangle-relationships", "right-triangle", "special-right-triangles", "45-45-90",
		),
		triangleFact(
			"tri:special:30-60-90:ratio",
			"What is the short-leg:long-leg:hypotenuse ratio in a 30-60-90 triangle?",
			"1:sqrt(3):2",
			"special-right-triangles",
			"triangle_ratio",
			"ratio",
			"In a 30-60-90 triangle, the side opposite 30 degrees is the short leg, the side opposite 60 degrees is short*sqrt(3), and the hypotenuse is twice the short leg.",
			"triangle-relationships", "right-triangle", "special-right-triangles", "30-60-90",
		),
		triangleFact(
			"tri:special:30-60-90:opposite-30",
			"In a 30-60-90 triangle, the side opposite 30 degrees is which side?",
			"short leg",
			"special-right-triangles",
			"triangle_term",
			"side-name",
			"The 30-degree angle is across from the shortest side.",
			"triangle-relationships", "right-triangle", "special-right-triangles", "30-60-90",
		),
		triangleFact(
			"tri:special:30-60-90:opposite-60",
			"In a 30-60-90 triangle, the side opposite 60 degrees is which side?",
			"long leg",
			"special-right-triangles",
			"triangle_term",
			"side-name",
			"The 60-degree angle is across from the longer leg, whose length is short*sqrt(3).",
			"triangle-relationships", "right-triangle", "special-right-triangles", "30-60-90",
		),
		triangleFact(
			"tri:special:45-45-90:hypotenuse-from-leg-5",
			"In a 45-45-90 triangle with leg 5, what is the hypotenuse?",
			"5sqrt(2)",
			"special-right-triangles",
			"triangle_length",
			"ratio",
			"A 45-45-90 triangle has ratio 1:1:sqrt(2), so the hypotenuse is 5sqrt(2).",
			"triangle-relationships", "right-triangle", "special-right-triangles", "45-45-90", "missing-side",
		),
		triangleFact(
			"tri:special:30-60-90:hypotenuse-from-short-4",
			"In a 30-60-90 triangle with short leg 4, what is the hypotenuse?",
			"8",
			"special-right-triangles",
			"exact_quantity",
			"ratio",
			"A 30-60-90 triangle has ratio 1:sqrt(3):2, so the hypotenuse is twice the short leg.",
			"triangle-relationships", "right-triangle", "special-right-triangles", "30-60-90", "missing-side",
		),
		triangleFact(
			"tri:special:30-60-90:long-from-short-4",
			"In a 30-60-90 triangle with short leg 4, what is the long leg?",
			"4sqrt(3)",
			"special-right-triangles",
			"triangle_length",
			"ratio",
			"A 30-60-90 triangle has ratio 1:sqrt(3):2, so the long leg is short*sqrt(3).",
			"triangle-relationships", "right-triangle", "special-right-triangles", "30-60-90", "missing-side",
		),
		triangleFact(
			"tri:trig:sin-ratio",
			"In a right triangle, sin(theta) is what side ratio?",
			"opposite/hypotenuse",
			"right-triangle-trig-ratios",
			"triangle_ratio",
			"sin",
			"Sine compares the side opposite theta to the hypotenuse.",
			"triangle-relationships", "right-triangle", "trig-foundations", "sohcahtoa",
		),
		triangleFact(
			"tri:trig:cos-ratio",
			"In a right triangle, cos(theta) is what side ratio?",
			"adjacent/hypotenuse",
			"right-triangle-trig-ratios",
			"triangle_ratio",
			"cos",
			"Cosine compares the side adjacent to theta to the hypotenuse.",
			"triangle-relationships", "right-triangle", "trig-foundations", "sohcahtoa",
		),
		triangleFact(
			"tri:trig:tan-ratio",
			"In a right triangle, tan(theta) is what side ratio?",
			"opposite/adjacent",
			"right-triangle-trig-ratios",
			"triangle_ratio",
			"tan",
			"Tangent compares the side opposite theta to the adjacent leg.",
			"triangle-relationships", "right-triangle", "trig-foundations", "sohcahtoa",
		),
		triangleFact(
			"tri:similar:side-ratios",
			"In similar triangles, matching side ratios stay what?",
			"proportional",
			"similar-triangles",
			"triangle_term",
			"similarity",
			"Similar triangles have equal matching angles, so corresponding side lengths scale by the same factor.",
			"triangle-relationships", "similar-triangles", "scale-factor",
		),
	}
	return uniqueFacts(facts)
}

func BuildTriangleLessonFacts(lesson TriangleLesson) []Fact {
	switch lesson {
	case "", TriangleLessonConcepts:
		return filterFacts(BuildTriangleFacts(), func(fact Fact) bool {
			switch fact.ID {
			case "tri:concept:right-angle", "tri:side:hypotenuse", "tri:side:opposite", "tri:side:adjacent":
				return true
			default:
				return false
			}
		})
	case TriangleLessonAngleSum:
		return filterFacts(BuildTriangleFacts(), func(fact Fact) bool {
			return fact.Operator == "angle-sum"
		})
	case TriangleLessonPythagorean:
		return filterFacts(BuildTriangleFacts(), func(fact Fact) bool {
			return hasRelationshipTag(fact, "pythagorean-theorem")
		})
	case TriangleLessonSpecial:
		return filterFacts(BuildTriangleFacts(), func(fact Fact) bool {
			return hasRelationshipTag(fact, "special-right-triangles")
		})
	case TriangleLessonSOHCAHTOA:
		return filterFacts(BuildTriangleFacts(), func(fact Fact) bool {
			return hasRelationshipTag(fact, "sohcahtoa")
		})
	case TriangleLessonMixed:
		return BuildTriangleFacts()
	default:
		log.Fatalf("unknown triangles lesson %q; use concepts, angle-sum, pythagorean, special-right, sohcahtoa, or mixed", lesson)
		return BuildTriangleLessonFacts(TriangleLessonConcepts)
	}
}

func triangleFact(id, prompt, answer, family, kind, operator, explanation string, tags ...string) Fact {
	return Fact{
		ID:               id,
		Prompt:           prompt,
		Answer:           answer,
		Explanation:      explanation,
		RelationshipTags: tags,
		Family:           family,
		Kind:             kind,
		Operator:         operator,
	}
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
	fact := t.pickFactAvoidingRecent()
	t.rememberRecentFact(fact.ID)
	return fact
}

func (t *Trainer) pickFactAvoidingRecent() Fact {
	recent := map[string]bool{}
	for _, id := range t.recentFactIDsForCooldown() {
		recent[id] = true
	}
	fact, ok := t.pickWeightedFact(func(candidate Fact) bool {
		return !recent[candidate.ID]
	})
	if ok {
		return fact
	}
	fact, _ = t.pickWeightedFact(func(Fact) bool {
		return true
	})
	return fact
}

func (t *Trainer) pickWeightedFact(keep func(Fact) bool) (Fact, bool) {
	total := 0
	weights := make([]int, len(t.Facts))
	for i, fact := range t.Facts {
		if !keep(fact) {
			continue
		}
		weight := t.Weight(fact.ID)
		weights[i] = weight
		total += weight
	}
	if total == 0 {
		return Fact{}, false
	}
	pick := t.Rand.Intn(total)
	for i, weight := range weights {
		if weight == 0 {
			continue
		}
		if pick < weight {
			return t.Facts[i], true
		}
		pick -= weight
	}
	return t.Facts[len(t.Facts)-1], true
}

func (t *Trainer) rememberRecentFact(id string) {
	limit := t.recentFactLimit()
	if limit == 0 || id == "" {
		t.RecentFactIDs = nil
		return
	}
	t.RecentFactIDs = append(t.RecentFactIDs, id)
	if len(t.RecentFactIDs) > limit {
		t.RecentFactIDs = t.RecentFactIDs[len(t.RecentFactIDs)-limit:]
	}
}

func (t *Trainer) recentFactLimit() int {
	if len(t.Facts) <= 1 {
		return 0
	}
	return min(3, max(1, len(t.Facts)/2))
}

func (t *Trainer) recentFactIDsForCooldown() []string {
	limit := t.recentFactLimit()
	if limit == 0 || len(t.RecentFactIDs) <= limit {
		return t.RecentFactIDs
	}
	return t.RecentFactIDs[len(t.RecentFactIDs)-limit:]
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

func (t *Trainer) Record(id string, answer string, elapsed time.Duration) AttemptResult {
	fact := t.ByID[id]
	stats := t.Progress.Facts[id]
	if stats == nil {
		stats = &FactStats{}
		t.Progress.Facts[id] = stats
	}
	correct := answerMatches(fact, answer)
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

func answerMatches(fact Fact, answer string) bool {
	answer = normalizeAnswer(answer)
	if answer == "" {
		return false
	}
	switch fact.Kind {
	case "multiply", "divide":
		return answer == normalizeAnswer(fact.Answer)
	case "fraction_to_decimal":
		return normalizeDecimal(answer) == normalizeDecimal(fact.Answer)
	case "decimal_to_fraction":
		gotN, gotD, ok := parseFraction(answer)
		if !ok {
			return false
		}
		wantN, wantD, ok := parseFraction(fact.Answer)
		if !ok {
			return false
		}
		return gotN == wantN && gotD == wantD
	case "fraction_to_percent", "percent_to_fraction", "decimal_to_percent", "percent_to_decimal":
		return equivalentValue(answer, fact.Answer, true)
	case "percent_relationship":
		return percentRelationshipMatches(fact, answer)
	case "exact_quantity":
		return exactQuantityMatches(answer, fact.Answer)
	case "exponent_log_number":
		return exactQuantityMatches(answer, fact.Answer)
	case "exponent_log_form":
		return logEquationsMatch(answer, fact.Answer)
	case "log_exponent_form":
		return exponentEquationsMatch(answer, fact.Answer)
	case "root_form":
		return rootEquationsMatch(answer, fact.Answer)
	case "algebra_identity":
		return normalizeAlgebraIdentity(answer) == normalizeAlgebraIdentity(fact.Answer)
	case "algebra_identity_concept":
		return algebraIdentityConceptMatches(answer, fact.Answer)
	case "algebra_pattern_name":
		return algebraPatternNamesMatch(answer, fact.Answer)
	case "triangle_formula":
		return triangleFormulasMatch(answer, fact.Answer)
	case "triangle_ratio", "triangle_length":
		return triangleRatiosMatch(answer, fact.Answer)
	case "triangle_term":
		return triangleTermsMatch(answer, fact.Answer)
	case "exponent_log_role":
		return exponentLogRolesMatch(answer, fact.Answer)
	case "unit_circle_radian":
		return unitCircleRadiansMatch(answer, fact.Answer)
	case "unit_circle_degree":
		return unitCircleDegreesMatch(answer, fact.Answer)
	case "unit_circle_value":
		return unitCircleValuesMatch(answer, fact.Answer)
	case "unit_circle_point":
		return unitCirclePointsMatch(answer, fact.Answer)
	case "unit_circle_quadrant":
		return unitCircleQuadrantsMatch(answer, fact.Answer)
	case "unit_circle_sign":
		return unitCircleSignsMatch(answer, fact.Answer)
	case "unit_circle_axis":
		return unitCircleAxesMatch(answer, fact.Answer)
	case "unit_circle_ratio":
		return unitCircleRatiosMatch(answer, fact.Answer)
	case "unit_circle_function":
		return unitCircleFunctionsMatch(answer, fact.Answer)
	case "unit_circle_point_order":
		return unitCirclePointOrdersMatch(answer, fact.Answer)
	case "unit_circle_triangle":
		return unitCircleTrianglesMatch(answer, fact.Answer)
	default:
		return answer == normalizeAnswer(fact.Answer)
	}
}

func normalizeAnswer(answer string) string {
	answer = strings.TrimSpace(strings.ToLower(answer))
	answer = strings.ReplaceAll(answer, " ", "")
	if strings.HasPrefix(answer, "0.") {
		answer = strings.TrimPrefix(answer, "0")
	}
	return answer
}

func normalizeAlgebraIdentity(answer string) string {
	answer = normalizeAnswer(answer)
	answer = strings.ReplaceAll(answer, "**", "^")
	answer = strings.ReplaceAll(answer, "*", "")
	return answer
}

func algebraIdentityConceptMatches(answer, expected string) bool {
	got, ok := normalizeAlgebraIdentityConcept(answer)
	if !ok {
		return false
	}
	want, ok := normalizeAlgebraIdentityConcept(expected)
	if !ok {
		return false
	}
	return got == want
}

func normalizeAlgebraIdentityConcept(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "identity", "alwaystrue", "trueforallvalues", "trueforallallowedvalues", "equationtrueforallvalues", "statementtrueforallvalues":
		return "identity", true
	case "expand", "expansion", "multiplyout", "writeasasumofterms", "rewriteasasumofterms", "sumofterms":
		return "expand", true
	case "factor", "factoring", "factorout", "writeasaproductoffactors", "rewriteasaproductoffactors", "productoffactors":
		return "factor", true
	case "distribute", "distribution", "distributive", "multiplyintoeachterm", "multiplytoeachterm", "applymultiplicationtoeachterm":
		return "distribute", true
	case "equivalent", "equivalentexpression", "equivalentexpressions", "samevalue", "samevalueforthesameinputs", "sameoutputforthesameinputs":
		return "equivalent-expression", true
	default:
		return "", false
	}
}

func algebraPatternNamesMatch(answer, expected string) bool {
	got, ok := normalizeAlgebraPatternName(answer)
	if !ok {
		return false
	}
	want, ok := normalizeAlgebraPatternName(expected)
	if !ok {
		return false
	}
	return got == want
}

func normalizeAlgebraPatternName(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "differenceofsquares", "diffofsquares":
		return "difference-of-squares", true
	case "perfectsquaretrinomial", "perfectsquare", "squaretrinomial":
		return "perfect-square-trinomial", true
	default:
		if value == "" {
			return "", false
		}
		return value, true
	}
}

func triangleFormulasMatch(answer, expected string) bool {
	return normalizeAlgebraIdentity(answer) == normalizeAlgebraIdentity(expected)
}

func triangleRatiosMatch(answer, expected string) bool {
	got := normalizeTriangleRatio(answer)
	want := normalizeTriangleRatio(expected)
	return got != "" && got == want
}

func normalizeTriangleRatio(value string) string {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "**", "^")
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, ",", ":")
	value = strings.ReplaceAll(value, "over", "/")
	value = strings.ReplaceAll(value, "hypotenuse", "hyp")
	value = strings.ReplaceAll(value, "opposite", "opp")
	value = strings.ReplaceAll(value, "adjacent", "adj")
	value = strings.ReplaceAll(value, "sqrt(2)", "sqrt2")
	value = strings.ReplaceAll(value, "sqrt(3)", "sqrt3")
	return value
}

func triangleTermsMatch(answer, expected string) bool {
	got, ok := normalizeTriangleTerm(answer)
	if !ok {
		return false
	}
	want, ok := normalizeTriangleTerm(expected)
	if !ok {
		return false
	}
	return got == want
}

func normalizeTriangleTerm(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "hypotenuse", "hyp":
		return "hypotenuse", true
	case "opposite", "oppositeside":
		return "opposite", true
	case "adjacent", "adjacentside", "adjacentleg":
		return "adjacent", true
	case "shortleg", "shortside", "short":
		return "short-leg", true
	case "longleg", "longside", "long":
		return "long-leg", true
	case "proportional", "same", "equalratios", "sameratio", "sameratios":
		return "proportional", true
	default:
		return "", false
	}
}

func normalizeDecimal(value string) string {
	value = normalizeAnswer(value)
	if strings.HasPrefix(value, ".") {
		value = "0" + value
	}
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if strings.HasPrefix(value, "0.") {
		return strings.TrimPrefix(value, "0")
	}
	return value
}

func parseFraction(value string) (int, int, bool) {
	value = normalizeAnswer(value)
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, 0, false
	}
	numerator, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	denominator, err := strconv.Atoi(parts[1])
	if err != nil || denominator == 0 {
		return 0, 0, false
	}
	divisor := gcd(abs(numerator), abs(denominator))
	numerator /= divisor
	denominator /= divisor
	if denominator < 0 {
		numerator *= -1
		denominator *= -1
	}
	return numerator, denominator, true
}

func decimalForFraction(numerator, denominator int) string {
	remainder := numerator % denominator
	whole := numerator / denominator
	if remainder == 0 {
		return strconv.Itoa(whole)
	}
	digits := strings.Builder{}
	for remainder != 0 {
		remainder *= 10
		digits.WriteString(strconv.Itoa(remainder / denominator))
		remainder %= denominator
	}
	if whole == 0 {
		return "." + digits.String()
	}
	return fmt.Sprintf("%d.%s", whole, digits.String())
}

func percentForFraction(numerator, denominator int) string {
	return decimalForFraction(numerator*100, denominator) + "%"
}

func reducedFractionString(numerator, denominator int) string {
	if denominator == 0 {
		return ""
	}
	divisor := gcd(abs(numerator), abs(denominator))
	numerator /= divisor
	denominator /= divisor
	if denominator < 0 {
		numerator *= -1
		denominator *= -1
	}
	if denominator == 1 {
		return strconv.Itoa(numerator)
	}
	return fmt.Sprintf("%d/%d", numerator, denominator)
}

func equivalentValue(answer, expected string, bareNumberMeansPercent bool) bool {
	got, ok := parseValue(answer, bareNumberMeansPercent)
	if !ok {
		return false
	}
	want, ok := parseValue(expected, strings.HasSuffix(normalizeAnswer(expected), "%"))
	if !ok {
		return false
	}
	return got.Cmp(want) == 0
}

func percentRelationshipMatches(fact Fact, answer string) bool {
	percent, base, ok := parsePercentOfExpression(answer)
	if ok {
		return percent.Cmp(big.NewRat(int64(fact.B), 1)) == 0 &&
			base.Cmp(big.NewRat(int64(fact.A), 1)) == 0
	}
	got, ok := parseQuantityRat(answer)
	if !ok {
		return false
	}
	want := percentOfRat(fact.A, fact.B)
	return got.Cmp(want) == 0
}

func parseValue(value string, bareNumberMeansPercent bool) (*big.Rat, bool) {
	value = normalizeAnswer(value)
	if value == "" {
		return nil, false
	}
	if strings.HasSuffix(value, "%") {
		rat, ok := parseNumberRat(strings.TrimSuffix(value, "%"))
		if !ok {
			return nil, false
		}
		return rat.Quo(rat, big.NewRat(100, 1)), true
	}
	if strings.Contains(value, "/") {
		numerator, denominator, ok := parseFraction(value)
		if !ok {
			return nil, false
		}
		return big.NewRat(int64(numerator), int64(denominator)), true
	}
	rat, ok := parseNumberRat(value)
	if !ok {
		return nil, false
	}
	if bareNumberMeansPercent && rat.Cmp(big.NewRat(1, 1)) >= 0 {
		return rat.Quo(rat, big.NewRat(100, 1)), true
	}
	return rat, true
}

func parsePercentOfExpression(value string) (*big.Rat, *big.Rat, bool) {
	value = normalizeAnswer(value)
	parts := strings.Split(value, "%of")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, nil, false
	}
	percent, ok := parseNumberRat(parts[0])
	if !ok {
		return nil, nil, false
	}
	base, ok := parseQuantityRat(parts[1])
	if !ok {
		return nil, nil, false
	}
	return percent, base, true
}

func parseQuantityRat(value string) (*big.Rat, bool) {
	value = normalizeAnswer(value)
	if value == "" || strings.HasSuffix(value, "%") {
		return nil, false
	}
	if strings.Contains(value, "/") {
		numerator, denominator, ok := parseFraction(value)
		if !ok {
			return nil, false
		}
		return big.NewRat(int64(numerator), int64(denominator)), true
	}
	return parseNumberRat(value)
}

func percentOfRat(percent, base int) *big.Rat {
	return new(big.Rat).Quo(big.NewRat(int64(percent*base), 1), big.NewRat(100, 1))
}

type ExponentLogEquation struct {
	Base     *big.Rat
	Exponent *big.Rat
	Value    *big.Rat
}

func exactQuantityMatches(answer, expected string) bool {
	got, ok := parseExpressionQuantityRat(answer)
	if !ok {
		return false
	}
	want, ok := parseExpressionQuantityRat(expected)
	if !ok {
		return false
	}
	return got.Cmp(want) == 0
}

func logEquationsMatch(answer, expected string) bool {
	got, ok := parseLogEquation(answer)
	if !ok {
		return false
	}
	want, ok := parseLogEquation(expected)
	if !ok {
		return false
	}
	return exponentLogEquationsMatch(got, want)
}

func exponentEquationsMatch(answer, expected string) bool {
	got, ok := parseExponentEquation(answer)
	if !ok {
		return false
	}
	want, ok := parseExponentEquation(expected)
	if !ok {
		return false
	}
	return exponentLogEquationsMatch(got, want)
}

func rootEquationsMatch(answer, expected string) bool {
	got, ok := parseRootEquation(answer)
	if !ok {
		return false
	}
	want, ok := parseRootEquation(expected)
	if !ok {
		return false
	}
	return exponentLogEquationsMatch(got, want)
}

func exponentLogEquationsMatch(got, want ExponentLogEquation) bool {
	return got.Base.Cmp(want.Base) == 0 &&
		got.Exponent.Cmp(want.Exponent) == 0 &&
		got.Value.Cmp(want.Value) == 0
}

func exponentLogRolesMatch(answer, expected string) bool {
	got, ok := normalizeExponentLogRole(answer)
	if !ok {
		return false
	}
	want, ok := normalizeExponentLogRole(expected)
	if !ok {
		return false
	}
	return got == want
}

func normalizeExponentLogRole(value string) (string, bool) {
	value = normalizeAnswer(value)
	switch value {
	case "base", "multiplier", "repeatedmultiplier":
		return "base", true
	case "exponent", "power":
		return "exponent", true
	case "value", "result", "output":
		return "value", true
	case "argument", "input":
		return "argument", true
	default:
		return "", false
	}
}

func parseLogEquation(value string) (ExponentLogEquation, bool) {
	value = normalizeAnswer(value)
	parts := strings.Split(value, "=")
	if len(parts) != 2 {
		return ExponentLogEquation{}, false
	}
	left := parts[0]
	if !strings.HasPrefix(left, "log") {
		return ExponentLogEquation{}, false
	}
	left = strings.TrimPrefix(left, "log")
	left = strings.TrimPrefix(left, "_")
	open := strings.Index(left, "(")
	close := strings.LastIndex(left, ")")
	if open <= 0 || close != len(left)-1 {
		return ExponentLogEquation{}, false
	}
	base, ok := parseExpressionQuantityRat(left[:open])
	if !ok {
		return ExponentLogEquation{}, false
	}
	argument, ok := parseExpressionQuantityRat(left[open+1 : close])
	if !ok {
		return ExponentLogEquation{}, false
	}
	exponent, ok := parseExpressionQuantityRat(parts[1])
	if !ok {
		return ExponentLogEquation{}, false
	}
	return ExponentLogEquation{Base: base, Exponent: exponent, Value: argument}, true
}

func parseExponentEquation(value string) (ExponentLogEquation, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "**", "^")
	parts := strings.Split(value, "=")
	if len(parts) != 2 {
		return ExponentLogEquation{}, false
	}
	left := parts[0]
	caret := strings.Index(left, "^")
	if caret <= 0 || caret == len(left)-1 {
		return ExponentLogEquation{}, false
	}
	base, ok := parseExpressionQuantityRat(left[:caret])
	if !ok {
		return ExponentLogEquation{}, false
	}
	exponent, ok := parseExpressionQuantityRat(left[caret+1:])
	if !ok {
		return ExponentLogEquation{}, false
	}
	result, ok := parseExpressionQuantityRat(parts[1])
	if !ok {
		return ExponentLogEquation{}, false
	}
	return ExponentLogEquation{Base: base, Exponent: exponent, Value: result}, true
}

func parseRootEquation(value string) (ExponentLogEquation, bool) {
	value = normalizeAnswer(value)
	parts := strings.Split(value, "=")
	if len(parts) != 2 {
		return ExponentLogEquation{}, false
	}
	indexText, radicandText, ok := parseRootLeft(parts[0])
	if !ok {
		return ExponentLogEquation{}, false
	}
	index, ok := parseExpressionQuantityRat(indexText)
	if !ok {
		return ExponentLogEquation{}, false
	}
	radicand, ok := parseExpressionQuantityRat(radicandText)
	if !ok {
		return ExponentLogEquation{}, false
	}
	result, ok := parseExpressionQuantityRat(parts[1])
	if !ok {
		return ExponentLogEquation{}, false
	}
	return ExponentLogEquation{Base: result, Exponent: index, Value: radicand}, true
}

func parseRootLeft(left string) (string, string, bool) {
	if strings.HasPrefix(left, "sqrt(") {
		radicand, ok := parenthesizedArgument(strings.TrimPrefix(left, "sqrt"))
		return "2", radicand, ok
	}
	if strings.HasPrefix(left, "squareroot(") {
		radicand, ok := parenthesizedArgument(strings.TrimPrefix(left, "squareroot"))
		return "2", radicand, ok
	}
	if strings.HasPrefix(left, "cuberoot(") {
		radicand, ok := parenthesizedArgument(strings.TrimPrefix(left, "cuberoot"))
		return "3", radicand, ok
	}
	if strings.HasPrefix(left, "root") {
		rest := strings.TrimPrefix(left, "root")
		rest = strings.TrimPrefix(rest, "_")
		open := strings.Index(rest, "(")
		if open <= 0 {
			return "", "", false
		}
		radicand, ok := parenthesizedArgument(rest[open:])
		return rest[:open], radicand, ok
	}
	rootPos := strings.Index(left, "root(")
	if rootPos <= 0 {
		return "", "", false
	}
	indexText := stripOrdinalSuffix(left[:rootPos])
	radicand, ok := parenthesizedArgument(left[rootPos+len("root"):])
	return indexText, radicand, ok
}

func parenthesizedArgument(value string) (string, bool) {
	open := strings.Index(value, "(")
	close := strings.LastIndex(value, ")")
	if open != 0 || close != len(value)-1 || close <= open+1 {
		return "", false
	}
	return value[open+1 : close], true
}

func stripOrdinalSuffix(value string) string {
	for _, suffix := range []string{"st", "nd", "rd", "th"} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return value
}

func parseExpressionQuantityRat(value string) (*big.Rat, bool) {
	return parseQuantityRat(trimOuterParens(value))
}

func unitCircleRadiansMatch(answer, expected string) bool {
	got, ok := parsePiMultiple(answer)
	if !ok {
		return false
	}
	want, ok := parsePiMultiple(expected)
	if !ok {
		return false
	}
	return got.Cmp(want) == 0
}

func unitCircleDegreesMatch(answer, expected string) bool {
	got, ok := parseDegree(answer)
	if !ok {
		return false
	}
	want, ok := parseDegree(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleValuesMatch(answer, expected string) bool {
	got, ok := normalizeUnitCircleValue(answer)
	if !ok {
		return false
	}
	want, ok := normalizeUnitCircleValue(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCirclePointsMatch(answer, expected string) bool {
	got, ok := normalizeUnitCirclePoint(answer)
	if !ok {
		return false
	}
	want, ok := normalizeUnitCirclePoint(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleQuadrantsMatch(answer, expected string) bool {
	got, ok := normalizeQuadrant(answer)
	if !ok {
		return false
	}
	want, ok := normalizeQuadrant(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleSignsMatch(answer, expected string) bool {
	got, ok := normalizeSign(answer)
	if !ok {
		return false
	}
	want, ok := normalizeSign(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleAxesMatch(answer, expected string) bool {
	got, ok := normalizeAxis(answer)
	if !ok {
		return false
	}
	want, ok := normalizeAxis(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleRatiosMatch(answer, expected string) bool {
	got, ok := normalizeRatio(answer)
	if !ok {
		return false
	}
	want, ok := normalizeRatio(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleFunctionsMatch(answer, expected string) bool {
	got, ok := normalizeTrigFunction(answer)
	if !ok {
		return false
	}
	want, ok := normalizeTrigFunction(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCirclePointOrdersMatch(answer, expected string) bool {
	got, ok := normalizePointOrder(answer)
	if !ok {
		return false
	}
	want, ok := normalizePointOrder(expected)
	if !ok {
		return false
	}
	return got == want
}

func unitCircleTrianglesMatch(answer, expected string) bool {
	got, ok := normalizeTriangle(answer)
	if !ok {
		return false
	}
	want, ok := normalizeTriangle(expected)
	if !ok {
		return false
	}
	return got == want
}

func parseDegree(value string) (int, bool) {
	value = normalizeAnswer(value)
	value = trimAnySuffix(value, []string{"degrees", "degree", "degs", "deg"})
	degrees, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return degrees, true
}

func parsePiMultiple(value string) (*big.Rat, bool) {
	value = normalizeAnswer(value)
	value = trimAnySuffix(value, []string{"radians", "radian", "rads", "rad"})
	value = strings.ReplaceAll(value, "*", "")
	if value == "0" {
		return big.NewRat(0, 1), true
	}
	parts := strings.Split(value, "pi")
	if len(parts) != 2 {
		return nil, false
	}
	coefficient := parts[0]
	numerator := 1
	switch coefficient {
	case "", "+":
		numerator = 1
	case "-":
		numerator = -1
	default:
		parsed, err := strconv.Atoi(coefficient)
		if err != nil {
			return nil, false
		}
		numerator = parsed
	}
	rat := big.NewRat(int64(numerator), 1)
	if parts[1] == "" {
		return rat, true
	}
	if !strings.HasPrefix(parts[1], "/") {
		return nil, false
	}
	denominator, err := strconv.Atoi(strings.TrimPrefix(parts[1], "/"))
	if err != nil || denominator == 0 {
		return nil, false
	}
	return rat.Quo(rat, big.NewRat(int64(denominator), 1)), true
}

func normalizeUnitCircleValue(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "*", "")
	value = strings.TrimPrefix(value, "+")
	switch value {
	case "undefined", "undef", "dne", "none":
		return "undefined", true
	}
	if rat, ok := parseQuantityRat(value); ok {
		return rat.RatString(), true
	}
	value = strings.ReplaceAll(value, "(", "")
	value = strings.ReplaceAll(value, ")", "")
	aliases := map[string]string{
		"sqrt2/2":   "sqrt2/2",
		"1/sqrt2":   "sqrt2/2",
		"sqrt2":     "sqrt2",
		"sqrt3/2":   "sqrt3/2",
		"sqrt3":     "sqrt3",
		"sqrt3/3":   "sqrt3/3",
		"1/sqrt3":   "sqrt3/3",
		"2sqrt3/3":  "2sqrt3/3",
		"2/sqrt3":   "2sqrt3/3",
		"-sqrt2/2":  "-sqrt2/2",
		"-1/sqrt2":  "-sqrt2/2",
		"-sqrt2":    "-sqrt2",
		"-sqrt3/2":  "-sqrt3/2",
		"-sqrt3":    "-sqrt3",
		"-sqrt3/3":  "-sqrt3/3",
		"-1/sqrt3":  "-sqrt3/3",
		"-2sqrt3/3": "-2sqrt3/3",
		"-2/sqrt3":  "-2sqrt3/3",
	}
	canonical, ok := aliases[value]
	return canonical, ok
}

func normalizeUnitCirclePoint(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.TrimPrefix(value, "(")
	value = strings.TrimSuffix(value, ")")
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return "", false
	}
	x, ok := normalizeUnitCircleValue(parts[0])
	if !ok {
		return "", false
	}
	y, ok := normalizeUnitCircleValue(parts[1])
	if !ok {
		return "", false
	}
	return x + "," + y, true
}

func normalizeQuadrant(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.TrimPrefix(value, "quadrant")
	value = strings.TrimPrefix(value, "quad")
	value = strings.TrimPrefix(value, "q")
	switch value {
	case "1", "i":
		return "I", true
	case "2", "ii":
		return "II", true
	case "3", "iii":
		return "III", true
	case "4", "iv":
		return "IV", true
	default:
		return "", false
	}
}

func normalizeSign(value string) (string, bool) {
	value = normalizeAnswer(value)
	switch value {
	case "+", "plus", "positive", "pos":
		return "+", true
	case "-", "minus", "negative", "neg":
		return "-", true
	case "0", "zero":
		return "0", true
	default:
		return "", false
	}
}

func normalizeAxis(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "-", "")
	value = strings.TrimSuffix(value, "coordinate")
	value = strings.TrimSuffix(value, "axis")
	switch value {
	case "x":
		return "x", true
	case "y":
		return "y", true
	default:
		return "", false
	}
}

func normalizeRatio(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "theta", "")
	value = strings.ReplaceAll(value, "(", "")
	value = strings.ReplaceAll(value, ")", "")
	switch value {
	case "y/x", "sin/cos", "sine/cosine":
		return "y/x", true
	case "x/y", "cos/sin", "cosine/sine":
		return "x/y", true
	default:
		return "", false
	}
}

func normalizeTrigFunction(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.TrimSuffix(value, "(theta)")
	switch value {
	case "sin", "sine":
		return "sin", true
	case "cos", "cosine":
		return "cos", true
	case "tan", "tangent":
		return "tan", true
	case "cot", "cotangent":
		return "cot", true
	case "sec", "secant":
		return "sec", true
	case "csc", "cosecant":
		return "csc", true
	default:
		return "", false
	}
}

func normalizePointOrder(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.ReplaceAll(value, "theta", "")
	value = strings.ReplaceAll(value, "(", "")
	value = strings.ReplaceAll(value, ")", "")
	switch value {
	case "cos,sin", "cosine,sine", "x,y":
		return "cos,sin", true
	case "sin,cos", "sine,cosine", "y,x":
		return "sin,cos", true
	default:
		return "", false
	}
}

func normalizeTriangle(value string) (string, bool) {
	value = normalizeAnswer(value)
	value = strings.TrimSuffix(value, "triangle")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "/", "")
	value = strings.ReplaceAll(value, ",", "")
	switch value {
	case "306090":
		return "30-60-90", true
	case "454590":
		return "45-45-90", true
	case "axis":
		return "axis", true
	default:
		return "", false
	}
}

func trimOuterParens(value string) string {
	value = normalizeAnswer(value)
	for len(value) >= 2 && value[0] == '(' && value[len(value)-1] == ')' && outerParensWrap(value) {
		value = value[1 : len(value)-1]
	}
	return value
}

func outerParensWrap(value string) bool {
	depth := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 || (depth == 0 && i != len(value)-1) {
				return false
			}
		}
	}
	return depth == 0
}

func trimAnySuffix(value string, suffixes []string) string {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return value
}

func parseNumberRat(value string) (*big.Rat, bool) {
	value = normalizeDecimal(value)
	if strings.HasPrefix(value, ".") {
		value = "0" + value
	}
	if value == "" {
		return nil, false
	}
	rat, ok := new(big.Rat).SetString(value)
	return rat, ok
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
.feedback { min-height: 54px; width: min(720px, 100%); font-size: 20px; font-weight: 650; line-height: 1.35; text-align: center; }
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
      <input id="answer" name="answer" inputmode="decimal" autofocus>
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
  const value = answer.value.trim();
  if (!value) return;
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
    feedback.textContent = "Missed: " + result.fact.prompt + " = " + result.answer + (result.fact.explanation ? ". " + result.fact.explanation : "");
  }
  await summary();
  setTimeout(nextFact, result.correct ? 350 : 900);
});

summary();
nextFact();
</script>
</body>
</html>`))
