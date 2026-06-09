# Math Relationship Trainer

Adaptive math practice for building relationship intuition. Arithmetic facts are still available, but the main conceptual modes train connections like parts-of-a-whole, percent-as-multiplier, `a% of b = b% of a`, `(cos(theta), sin(theta))`, and `b^x = y <=> log_b(y) = x`.

## CLI

```sh
go run . -minutes 10
```

The CLI times each answer, records misses, and prints the current weak facts when the session ends.

Fraction/decimal mode:

```sh
go run . -mode fractions -minutes 10
```

Percentage mode:

```sh
go run . -mode percentages -minutes 10
```

Percentage relationship mode:

```sh
go run . -mode percent-relations -minutes 10
```

Unit circle mode:

```sh
go run . -mode unit-circle -minutes 10
```

Exponent/log mode:

```sh
go run . -mode exponents-logs -minutes 10
```

All relationship modes together:

```sh
go run . -mode relationships -minutes 10
```

## Web

```sh
go run . web -addr 127.0.0.1:8080
```

Open `http://127.0.0.1:8080`.

Fraction/decimal mode:

```sh
go run . web -mode fractions -addr 127.0.0.1:8080
```

Percentage mode:

```sh
go run . web -mode percentages -addr 127.0.0.1:8080
```

Percentage relationship mode:

```sh
go run . web -mode percent-relations -addr 127.0.0.1:8080
```

Unit circle mode:

```sh
go run . web -mode unit-circle -addr 127.0.0.1:8080
```

Exponent/log mode:

```sh
go run . web -mode exponents-logs -addr 127.0.0.1:8080
```

All relationship modes together:

```sh
go run . web -mode relationships -addr 127.0.0.1:8080
```

## Progress

Progress is stored in `~/.math-study-progress.json` by default. Pass `-progress ./progress.json` to keep a local file instead.

Facts that are missed or slow are weighted more heavily. Facts answered correctly several times without misses or slow responses are treated as mastered and appear less often.

Relationship mode combines the conceptual decks without arithmetic fact practice: fractions, percentages, percent swaps, unit circle, and exponents/logs.

Fraction mode trains parts-of-a-whole relationships, numerator/denominator roles, fraction-as-division, fraction/decimal equivalence, complements to one, and reciprocals. For fraction-to-decimal prompts, `.125` and `0.125` are both accepted. Equivalent quantities like `2/8`, `.25`, and `0.25` can satisfy exact relationship prompts for `1/4`.

Percentage mode trains percent-as-multiplier and percent-means-per-100 relationships, plus fraction/decimal/percent equivalence. Equivalent forms like `25%`, `25`, `.25`, `0.25`, and `1/4` are accepted for the same value where the prompt is asking for an equivalent percentage value.

Percentage relationship mode trains the swap `a% of b = b% of a`, so prompts like `20% of 35` expect `35% of 20` or the numeric value `7`.

Unit circle mode trains the relationship web behind the circle: degree/radian conversions, `(cos, sin)` coordinates, sine-as-y, cosine-as-x, tangent-as-y-over-x, reference angles, quadrant signs, special triangles, cofunction relationships, and reciprocal functions. Each relationship drill carries an explanation and tags such as `sin-is-y`, `cos-is-x`, `tan-is-y-over-x`, `quadrant-signs`, and `reference-angles`. Exact radical answers like `sqrt(3)/2` are expected instead of decimal approximations.

Exponent/log mode trains exact relationships like `2^5 = 32 <=> log_2(32) = 5`, including negative exponents, missing bases, missing exponents, missing values, inverse forms like `log_2(2^5)` and `2^(log_2(32))`, and concept prompts like “a logarithm asks for the exponent.”
