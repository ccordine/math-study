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
	Facts    []Fact
	ByID     map[string]Fact
	Progress Progress
	Rand     *rand.Rand
	Path     string
	Mode     Mode
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

const (
	ModeArithmetic       Mode = "arithmetic"
	ModeFractions        Mode = "fractions"
	ModePercentages      Mode = "percentages"
	ModePercentRelations Mode = "percent-relations"
	ModeUnitCircle       Mode = "unit-circle"
	ModeExponentsLogs    Mode = "exponents-logs"
	ModeRelationships    Mode = "relationships"
	ModeMixed            Mode = "mixed"
)

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
	mode := fs.String("mode", string(ModeArithmetic), "practice mode: arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, relationships, mixed")
	minutes := fs.Int("minutes", 10, "session length in minutes")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	trainer := mustTrainer(*min, *max, parseMode(*mode), *progress)
	reader := bufio.NewReader(os.Stdin)
	deadline := time.Now().Add(time.Duration(*minutes) * time.Minute)

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

func webCmd(args []string) {
	fs := flag.NewFlagSet("math-study web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	min := fs.Int("min", 2, "smallest multiplication factor")
	max := fs.Int("max", 12, "largest multiplication factor")
	mode := fs.String("mode", string(ModeArithmetic), "practice mode: arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, relationships, mixed")
	progress := fs.String("progress", defaultProgressPath(), "progress JSON path")
	_ = fs.Parse(args)

	trainer := mustTrainer(*min, *max, parseMode(*mode), *progress)
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

func mustTrainer(min, max int, mode Mode, path string) *Trainer {
	if min < 1 || max < min {
		log.Fatalf("invalid range: min=%d max=%d", min, max)
	}
	trainer, err := NewTrainer(min, max, mode, path)
	if err != nil {
		log.Fatal(err)
	}
	return trainer
}

func parseMode(mode string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(mode))) {
	case ModeArithmetic, ModeFractions, ModePercentages, ModePercentRelations, ModeUnitCircle, ModeExponentsLogs, ModeRelationships, ModeMixed:
		return Mode(strings.ToLower(strings.TrimSpace(mode)))
	case "fraction-relations", "fraction-relationships":
		return ModeFractions
	case "percentage-relations", "percentage-relationships", "percentrelationships":
		return ModePercentRelations
	case "percentrelations":
		return ModePercentRelations
	case "unitcircle", "circle", "trig", "trigonometry":
		return ModeUnitCircle
	case "exponents", "exponent", "logs", "log", "logarithms", "logarithm", "exponent-logs", "exponentslogs", "log-relations", "exponent-relations":
		return ModeExponentsLogs
	case "relations", "math-relations", "math-relationships", "relationship-trainer":
		return ModeRelationships
	default:
		log.Fatalf("unknown mode %q; use arithmetic, fractions, percentages, percent-relations, unit-circle, exponents-logs, relationships, or mixed", mode)
		return ModeArithmetic
	}
}

func NewTrainer(min, max int, mode Mode, path string) (*Trainer, error) {
	facts := BuildFacts(min, max, mode)
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
	if mode == ModeUnitCircle || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildUnitCircleFacts()...)
	}
	if mode == ModeExponentsLogs || mode == ModeRelationships || mode == ModeMixed {
		facts = append(facts, BuildExponentLogFacts()...)
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
	return facts
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
