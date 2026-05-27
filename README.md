# Math Study

Adaptive arithmetic practice for multiplication facts from `2 x 2` through `12 x 12`, plus the matching division facts. It also has fraction and percentage modes for common relationships like `1/8 = .125 = 12.5%`.

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

## Progress

Progress is stored in `~/.math-study-progress.json` by default. Pass `-progress ./progress.json` to keep a local file instead.

Facts that are missed or slow are weighted more heavily. Facts answered correctly several times without misses or slow responses are treated as mastered and appear less often.

Fraction mode trains both directions for reduced fractions with denominators `2`, `4`, `5`, `8`, `10`, and `16`. For fraction-to-decimal prompts, `.125` and `0.125` are both accepted. For decimal-to-fraction prompts, equivalent fractions like `2/16` are accepted for `1/8`.

Percentage mode trains fraction-to-percent, percent-to-fraction, decimal-to-percent, and percent-to-decimal for the same core families. For percentage relationships, equivalent forms like `25%`, `25`, `.25`, `0.25`, and `1/4` are accepted for the same value.
