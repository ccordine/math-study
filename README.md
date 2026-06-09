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
go run . -mode unit-circle -lesson quadrants -minutes 10
```

Exponent/log mode:

```sh
go run . -mode exponents-logs -minutes 10
go run . -mode root-exponent-log -minutes 10
```

Algebra identities mode:

```sh
go run . -mode algebra-identities -minutes 10
go run . -mode algebra-identities -lesson expand -minutes 10
go run . -mode algebra-identities -lesson factor -minutes 10
go run . -mode algebra-identities -lesson recognize -minutes 10
```

Triangle relationships mode:

```sh
go run . -mode triangles -minutes 10
go run . -mode triangles -lesson angle-sum -minutes 10
go run . -mode triangles -lesson pythagorean -minutes 10
go run . -mode triangles -lesson special-right -minutes 10
go run . -mode triangles -lesson sohcahtoa -minutes 10
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
go run . web -mode unit-circle -lesson quadrants -addr 127.0.0.1:8080
```

Exponent/log mode:

```sh
go run . web -mode exponents-logs -addr 127.0.0.1:8080
go run . web -mode root-exponent-log -addr 127.0.0.1:8080
```

Algebra identities mode:

```sh
go run . web -mode algebra-identities -addr 127.0.0.1:8080
go run . web -mode algebra-identities -lesson expand -addr 127.0.0.1:8080
```

Triangle relationships mode:

```sh
go run . web -mode triangles -addr 127.0.0.1:8080
go run . web -mode triangles -lesson pythagorean -addr 127.0.0.1:8080
```

All relationship modes together:

```sh
go run . web -mode relationships -addr 127.0.0.1:8080
```

## Progress

Progress is stored in `~/.math-study-progress.json` by default. Pass `-progress ./progress.json` to keep a local file instead.

Facts that are missed or slow are weighted more heavily. Facts answered correctly several times without misses or slow responses are treated as mastered and appear less often.

Relationship mode combines the conceptual decks without arithmetic fact practice: fractions, percentages, percent swaps, unit circle, exponents/logs, algebra identities, and triangles.

Fraction mode trains parts-of-a-whole relationships, numerator/denominator roles, fraction-as-division, fraction/decimal equivalence, complements to one, and reciprocals. For fraction-to-decimal prompts, `.125` and `0.125` are both accepted. Equivalent quantities like `2/8`, `.25`, and `0.25` can satisfy exact relationship prompts for `1/4`.

Percentage mode trains percent-as-multiplier and percent-means-per-100 relationships, plus fraction/decimal/percent equivalence. Equivalent forms like `25%`, `25`, `.25`, `0.25`, and `1/4` are accepted for the same value where the prompt is asking for an equivalent percentage value.

Percentage relationship mode trains the swap `a% of b = b% of a`, so prompts like `20% of 35` expect `35% of 20` or the numeric value `7`.

Unit circle mode is a staged curriculum, not one giant deck. The default lesson is `concepts`; do not start with `mixed`.

Recommended unit-circle order:

```sh
go run . -mode unit-circle -lesson concepts
go run . -mode unit-circle -lesson quadrants
go run . -mode unit-circle -lesson reference-angles
go run . -mode unit-circle -lesson reference-values
go run . -mode unit-circle -lesson assemble
go run . -mode unit-circle -lesson tangent
go run . -mode unit-circle -lesson reciprocals
go run . -mode unit-circle -lesson mixed
```

Lessons:

- `concepts`: point order `(cos, sin)`, sine is y, cosine is x, tangent is y/x, and reciprocal function definitions.
- `quadrants`: quadrant identification and signs of sin/cos/tan; no exact trig values.
- `reference-angles`: angle to reference angle and angle to quadrant; no trig values.
- `reference-values`: first-quadrant values for 30, 45, and 60 degrees only.
- `assemble`: combine reference angle plus quadrant sign for sin/cos only.
- `tangent`: tangent ratio, tangent signs, tangent values, and undefined cases.
- `reciprocals`: sec/csc/cot values and reciprocal relationship prompts.
- `mixed`: full review after the pieces are individually comfortable.

Each relationship drill carries an explanation and tags such as `sin-is-y`, `cos-is-x`, `tan-is-y-over-x`, `quadrant-signs`, and `reference-angles`. Exact radical answers like `sqrt(3)/2` are expected instead of decimal approximations.

Exponent/log mode trains exact relationships like `2^5 = 32 <=> log_2(32) = 5`, including negative exponents, missing bases, missing exponents, missing values, inverse forms like `log_2(2^5)` and `2^(log_2(32))`, and concept prompts like "a logarithm asks for the exponent." For positive exponents it also trains root/exponent/log triads like `2^5=32 <=> log_2(32)=5 <=> root_5(32)=2`, with each source-target direction tracked as its own fact. Aliases include `root-exponent-log` and `roots-exponents-logs`.

Triangles mode trains relationships that support later trig and unit-circle intuition. The default lesson is `concepts`, which focuses on right-angle, hypotenuse, opposite, and adjacent relationships. Triangle facts are not mixed into unit-circle lessons.

Recommended triangles order:

```sh
go run . -mode triangles -lesson concepts
go run . -mode triangles -lesson angle-sum
go run . -mode triangles -lesson pythagorean
go run . -mode triangles -lesson special-right
go run . -mode triangles -lesson sohcahtoa
go run . -mode triangles -lesson mixed
```

Lessons:

- `concepts`: right angle, hypotenuse, opposite side, and adjacent side meanings.
- `angle-sum`: triangle angle sums and missing-angle prompts.
- `pythagorean`: `a^2+b^2=c^2`, missing hypotenuse, and missing leg prompts.
- `special-right`: 45-45-90 and 30-60-90 ratios, side identification, and simple missing sides.
- `sohcahtoa`: sine, cosine, and tangent as right-triangle side ratios.
- `mixed`: full review after the pieces are individually comfortable.

Algebra identities mode is also staged. The default lesson is `concepts`, which trains the meaning of identity, expand, factor, and equivalent expression before asking for transformations.

Recommended algebra-identities order:

```sh
go run . -mode algebra-identities -lesson concepts
go run . -mode algebra-identities -lesson expand
go run . -mode algebra-identities -lesson factor
go run . -mode algebra-identities -lesson recognize
go run . -mode algebra-identities -lesson mixed
```

Lessons:

- `concepts`: identity, expand, factor, and equivalent-expression vocabulary.
- `expand`: forward transformations like `(a+b)^2` to `a^2+2ab+b^2`.
- `factor`: reverse transformations like `a^2-b^2` to `(a+b)(a-b)`.
- `recognize`: pattern-name prompts such as difference of squares or perfect-square trinomial.
- `mixed`: full review, including the staged lessons plus broader patterns like cubes and binomial products.
