# tack

A Trello-style board and todo list for the terminal. One binary, data in a local SQLite file.

```
 tack  ·  Personal  ·  3 cards                                        ? keys  q quit
──────────────────────────────────────────────────────────────────────────────────
 To do  1                     │ Doing  1                     │ Done  1
 ──────────────────────────── │ ──────────────────────────── │ ────────────────────
                              │                              │
   Write report               │ ▌ Fix login on Safari        │   Buy milk
   !!  ◷ Tue  ● school        │ ▌ !!!  ◷ tomorrow  ☑ 1/2  ≡  │
                              │   ● bug                      │
```

## Install

```sh
go install github.com/DanielFrengl/tack@latest
```

## Interactive board

Run `tack` with no arguments to open the board. Press `?` to see every key.

| Keys | Action |
| --- | --- |
| `h j k l` / arrows | move between columns and cards |
| `n` | new card in the focused column (keeps asking; empty enter stops) |
| `enter` | open a card: notes, checklist, details |
| `H` `L` | move card to the column on the left / right |
| `J` `K` | reorder the card within its column |
| `space` | send the card to the last column (done) |
| `e` `p` `t` `#` | edit title, cycle priority, set due date, set labels |
| `a` / `A` | archive the card / open the archive |
| `D` | delete for good (asks first) |
| `/` | filter: words, `#label`, `!high`; `esc` clears |
| `b` | switch, add, rename or delete boards |
| `C` `R` `X` `<` `>` | add, rename, delete, move columns |

In a card: `d` edits notes (esc saves), `c` adds checklist items, `space` ticks one, `r` / `X` rename / remove it.

## Commands

```sh
tack add "Fix login" -c doing -l bug:red,backend -d fri -p high -n "repro on iOS"
tack ls                     # current board, grouped by column
tack ls "#bug !high"        # filtered
tack ls --all               # every board
tack search login           # across all boards
tack show 12
tack mv 12 done             # or: tack done 12
tack edit 12 -d none -l +urgent,-backend
tack check add 12 "write test"; tack check toggle 12 1
tack archive 12; tack unarchive 12; tack rm 12
tack board add Bynaps --cols "Backlog,In progress,Review,Done"
tack board use bynaps       # names match case-insensitively, prefixes work
tack -b school ls           # any command on another board
tack col add Review; tack label color bug red
```

Due dates take `2026-11-03`, `3.11.`, `today`, `tomorrow`, weekday names (`fri`), `+3d`, `+2w`, `+1m`, or `none`.
Label colors: red, orange, yellow, green, teal, blue, purple, pink, gray.

## Data

Cards live in `~/.local/share/tack/tack.db` (or `$XDG_DATA_HOME/tack/tack.db`). Set `TACK_DB` to use another file; `tack where` prints the path.
