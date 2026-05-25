# Math Study

Adaptive arithmetic practice for multiplication facts from `2 x 2` through `12 x 12`, plus the matching division facts.

## CLI

```sh
go run . -minutes 10
```

The CLI times each answer, records misses, and prints the current weak facts when the session ends.

## Web

```sh
go run . web -addr 127.0.0.1:8080
```

Open `http://127.0.0.1:8080`.

## Progress

Progress is stored in `~/.math-study-progress.json` by default. Pass `-progress ./progress.json` to keep a local file instead.

Facts that are missed or slow are weighted more heavily. Facts answered correctly several times without misses or slow responses are treated as mastered and appear less often.
