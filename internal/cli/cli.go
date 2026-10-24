// Package cli wires the tack commands; running tack with no command opens the board.
package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/DanielFrengl/tack/internal/meta"
	"github.com/DanielFrengl/tack/internal/store"
	"github.com/DanielFrengl/tack/internal/style"
	"github.com/DanielFrengl/tack/internal/tui"
)

var version = "dev"

type app struct {
	st        *store.Store
	boardFlag string
}

func Execute() error {
	a := &app{}
	root := &cobra.Command{
		Use:           "tack",
		Short:         "A Trello-style board and todo list for the terminal",
		Long:          "tack is a kanban board for the terminal.\nRun it with no arguments to open the interactive board, or use the commands below.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			path, err := store.DefaultPath()
			if err != nil {
				return err
			}
			a.st, err = store.Open(path)
			return err
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if a.st != nil {
				a.st.Close()
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			return tui.Run(a.st, b)
		},
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().StringVarP(&a.boardFlag, "board", "b", "", "board to use (default: the current board)")

	root.AddCommand(
		a.addCmd(), a.lsCmd(), a.searchCmd(), a.showCmd(), a.mvCmd(), a.doneCmd(), a.editCmd(),
		a.archiveCmd(true), a.archiveCmd(false), a.rmCmd(),
		a.boardCmd(), a.colCmd(), a.labelCmd(), a.checkCmd(), a.whereCmd(),
	)
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, style.ErrorFg.Render("error: ")+err.Error())
	}
	return err
}

// board resolves --board, falling back to the current board.
func (a *app) board() (store.Board, error) {
	if a.boardFlag != "" {
		return a.st.BoardByName(a.boardFlag)
	}
	return a.st.CurrentBoard()
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a card id", s)
	}
	return id, nil
}

func (a *app) cards(args []string) ([]store.Card, error) {
	var out []store.Card
	for _, s := range args {
		id, err := parseID(s)
		if err != nil {
			return nil, err
		}
		c, err := a.st.Card(id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyLabels handles "a,b" (replace), or "+a,-b" (add/remove). "name:color" sets a color.
func (a *app) applyLabels(c store.Card, spec string) error {
	ids := map[int64]bool{}
	items := splitList(spec)
	relative := len(items) > 0
	for _, it := range items {
		if it[0] != '+' && it[0] != '-' {
			relative = false
		}
	}
	if relative {
		for _, l := range c.Labels {
			ids[l.ID] = true
		}
	}
	for _, it := range items {
		op := byte('+')
		if it[0] == '+' || it[0] == '-' {
			op, it = it[0], it[1:]
		}
		name, color, _ := strings.Cut(it, ":")
		l, err := a.st.EnsureLabel(c.BoardID, name, strings.ToLower(color))
		if err != nil {
			return err
		}
		ids[l.ID] = op == '+'
	}
	var list []int64
	for id, on := range ids {
		if on {
			list = append(list, id)
		}
	}
	return a.st.SetCardLabels(c.ID, list)
}

func cardLine(c store.Card, now time.Time) string {
	line := style.Dim.Render(fmt.Sprintf("#%-4d", c.ID)) + " " + c.Title
	if m := style.MetaLine(c, now); m != "" {
		line += "  " + m
	}
	return line
}

func (a *app) addCmd() *cobra.Command {
	var col, labels, due, prio, notes string
	cmd := &cobra.Command{
		Use:     "add <title>",
		Aliases: []string{"a", "new"},
		Short:   "Add a card",
		Example: `  tack add "Buy milk"
  tack add "Fix login" -c doing -l bug,backend -d fri -p high
  tack add "Ship landing" -b Bynaps -d 2026-11-03 -n "hero + pricing section"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			cols, err := a.st.Columns(b.ID)
			if err != nil {
				return err
			}
			if len(cols) == 0 {
				return fmt.Errorf("board %q has no columns; add one with `tack col add`", b.Name)
			}
			target := cols[0]
			if col != "" {
				if target, err = a.st.ColumnByName(b.ID, col); err != nil {
					return err
				}
			}
			c, err := a.st.CreateCard(b.ID, target.ID, strings.Join(args, " "))
			if err != nil {
				return err
			}
			if due != "" {
				if c.Due, err = meta.ParseDue(due, time.Now()); err != nil {
					return err
				}
			}
			if prio != "" {
				if c.Priority, err = meta.ParsePriority(prio); err != nil {
					return err
				}
			}
			c.Description = notes
			if err := a.st.UpdateCard(c); err != nil {
				return err
			}
			if labels != "" {
				if err := a.applyLabels(c, labels); err != nil {
					return err
				}
			}
			c, _ = a.st.Card(c.ID)
			fmt.Printf("%s %s %s\n", style.SuccessFg.Render("added"), cardLine(c, time.Now()), style.Dim.Render("→ "+b.Name+" › "+target.Name))
			return nil
		},
	}
	cmd.Flags().StringVarP(&col, "col", "c", "", "column (default: first column)")
	cmd.Flags().StringVarP(&labels, "label", "l", "", "comma-separated labels, name:color to pick a color")
	cmd.Flags().StringVarP(&due, "due", "d", "", "due date: 2026-11-03, 3.11., today, tomorrow, fri, +3d")
	cmd.Flags().StringVarP(&prio, "prio", "p", "", "priority: low, medium, high")
	cmd.Flags().StringVarP(&notes, "notes", "n", "", "description")
	return cmd
}

func (a *app) lsCmd() *cobra.Command {
	var col, query string
	var archived, all bool
	cmd := &cobra.Command{
		Use:     "ls [query]",
		Aliases: []string{"list", "l"},
		Short:   "List cards, grouped by column",
		Example: `  tack ls
  tack ls -c doing
  tack ls "#bug !high"
  tack ls --all`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query = strings.Join(args, " ")
			var boards []store.Board
			if all {
				var err error
				if boards, err = a.st.Boards(); err != nil {
					return err
				}
			} else {
				b, err := a.board()
				if err != nil {
					return err
				}
				boards = []store.Board{b}
			}
			now := time.Now()
			for bi, b := range boards {
				if bi > 0 {
					fmt.Println()
				}
				fmt.Println(style.Bold.Render(b.Name))
				cols, err := a.st.Columns(b.ID)
				if err != nil {
					return err
				}
				cards, err := a.st.Cards(b.ID, archived)
				if err != nil {
					return err
				}
				for _, c := range cols {
					if col != "" && !strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(col)) {
						continue
					}
					var in []store.Card
					for _, card := range cards {
						if card.ColumnID == c.ID && meta.Matches(card, query) {
							in = append(in, card)
						}
					}
					if (query != "" || archived) && len(in) == 0 {
						continue
					}
					fmt.Printf("  %s %s\n", c.Name, style.Dim.Render(fmt.Sprintf("· %d", len(in))))
					for _, card := range in {
						fmt.Println("    " + cardLine(card, now))
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&col, "col", "c", "", "only this column")
	cmd.Flags().BoolVar(&archived, "archived", false, "list archived cards instead")
	cmd.Flags().BoolVar(&all, "all", false, "every board")
	return cmd
}

func (a *app) searchCmd() *cobra.Command {
	var archived bool
	cmd := &cobra.Command{
		Use:     "search <query>",
		Aliases: []string{"s", "find"},
		Short:   "Search cards on every board (#label and !priority narrow it down)",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			cards, err := a.st.AllCards(archived)
			if err != nil {
				return err
			}
			boards, _ := a.st.Boards()
			names := map[int64]string{}
			for _, b := range boards {
				cols, _ := a.st.Columns(b.ID)
				for _, c := range cols {
					names[c.ID] = b.Name + " › " + c.Name
				}
			}
			now := time.Now()
			n := 0
			for _, c := range cards {
				if meta.Matches(c, query) {
					fmt.Println(cardLine(c, now) + "  " + style.Dim.Render(names[c.ColumnID]))
					n++
				}
			}
			if n == 0 {
				fmt.Println(style.Dim.Render("no cards match " + strconv.Quote(query)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&archived, "archived", false, "search archived cards")
	return cmd
}

func (a *app) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <id>",
		Aliases: []string{"cat", "view"},
		Short:   "Show a card with its notes and checklist",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args)
			if err != nil {
				return err
			}
			c := cards[0]
			b, _ := a.st.Board(c.BoardID)
			cols, _ := a.st.Columns(c.BoardID)
			colName := ""
			for _, col := range cols {
				if col.ID == c.ColumnID {
					colName = col.Name
				}
			}
			now := time.Now()
			where := fmt.Sprintf("#%d · %s › %s", c.ID, b.Name, colName)
			if c.Archived {
				where += " · archived"
			}
			fmt.Println(style.Dim.Render(where))
			fmt.Println(style.Bold.Render(c.Title))
			if c.Priority > 0 {
				fmt.Printf("%s %s %s\n", style.Dim.Render("priority"), meta.PriorityNames[c.Priority], style.Priority(c.Priority))
			}
			if c.Due != nil {
				fmt.Printf("%s %s  %s\n", style.Dim.Render("due     "), c.Due.Format("Mon Jan 2 2006"), style.Due(*c.Due, now))
			}
			if len(c.Labels) > 0 {
				fmt.Printf("%s %s\n", style.Dim.Render("labels  "), style.Labels(c.Labels))
			}
			if strings.TrimSpace(c.Description) != "" {
				fmt.Println()
				fmt.Println(c.Description)
			}
			if len(c.Checklist) > 0 {
				fmt.Println()
				fmt.Println(style.Dim.Render(fmt.Sprintf("checklist %d/%d", c.ChecklistDone(), len(c.Checklist))))
				for i, it := range c.Checklist {
					box := "[ ]"
					text := it.Text
					if it.Done {
						box = style.SuccessFg.Render("[x]")
						text = style.Dim.Render(text)
					}
					fmt.Printf("  %d %s %s\n", i+1, box, text)
				}
			}
			return nil
		},
	}
}

func (a *app) mvCmd() *cobra.Command {
	var top bool
	cmd := &cobra.Command{
		Use:     "mv <id>... <column>",
		Aliases: []string{"move"},
		Short:   "Move cards to another column",
		Example: "  tack mv 12 doing\n  tack mv 12 13 done",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args[:len(args)-1])
			if err != nil {
				return err
			}
			for _, c := range cards {
				col, err := a.st.ColumnByName(c.BoardID, args[len(args)-1])
				if err != nil {
					return err
				}
				idx := -1
				if top {
					idx = 0
				}
				if err := a.st.MoveCard(c.ID, col.ID, idx); err != nil {
					return err
				}
				fmt.Printf("moved #%d %s → %s\n", c.ID, c.Title, col.Name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&top, "top", false, "put the card at the top of the column")
	return cmd
}

func (a *app) doneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <id>...",
		Short: "Move cards to the board's last column",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args)
			if err != nil {
				return err
			}
			for _, c := range cards {
				cols, err := a.st.Columns(c.BoardID)
				if err != nil {
					return err
				}
				last := cols[len(cols)-1]
				if err := a.st.MoveCard(c.ID, last.ID, -1); err != nil {
					return err
				}
				fmt.Printf("%s #%d %s → %s\n", style.SuccessFg.Render("✓"), c.ID, c.Title, last.Name)
			}
			return nil
		},
	}
}

func (a *app) editCmd() *cobra.Command {
	var title, notes, due, prio, labels string
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change a card's title, notes, due date, priority or labels",
		Example: `  tack edit 12 --title "Fix login on Safari"
  tack edit 12 -d none -p low
  tack edit 12 -l +urgent,-backend`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args)
			if err != nil {
				return err
			}
			c := cards[0]
			f := cmd.Flags()
			if f.Changed("title") {
				c.Title = title
			}
			if f.Changed("notes") {
				c.Description = notes
			}
			if f.Changed("due") {
				if c.Due, err = meta.ParseDue(due, time.Now()); err != nil {
					return err
				}
			}
			if f.Changed("prio") {
				if c.Priority, err = meta.ParsePriority(prio); err != nil {
					return err
				}
			}
			if err := a.st.UpdateCard(c); err != nil {
				return err
			}
			if f.Changed("label") {
				if err := a.applyLabels(c, labels); err != nil {
					return err
				}
			}
			c, _ = a.st.Card(c.ID)
			fmt.Println("updated " + cardLine(c, time.Now()))
			return nil
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", "new title")
	cmd.Flags().StringVarP(&notes, "notes", "n", "", "new description")
	cmd.Flags().StringVarP(&due, "due", "d", "", "due date, or none")
	cmd.Flags().StringVarP(&prio, "prio", "p", "", "priority: none, low, medium, high")
	cmd.Flags().StringVarP(&labels, "label", "l", "", "labels: a,b replaces; +a,-b adds/removes")
	return cmd
}

func (a *app) archiveCmd(archive bool) *cobra.Command {
	use, short, verb := "archive <id>...", "Archive cards (hide them from the board)", "archived"
	if !archive {
		use, short, verb = "unarchive <id>...", "Bring archived cards back", "restored"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args)
			if err != nil {
				return err
			}
			for _, c := range cards {
				if err := a.st.SetArchived(c.ID, archive); err != nil {
					return err
				}
				fmt.Printf("%s #%d %s\n", verb, c.ID, c.Title)
			}
			return nil
		},
	}
}

func (a *app) rmCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm <id>...",
		Aliases: []string{"delete"},
		Short:   "Delete cards for good (archive keeps them)",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args)
			if err != nil {
				return err
			}
			for _, c := range cards {
				if !yes && !confirm(fmt.Sprintf("delete #%d %q?", c.ID, c.Title)) {
					continue
				}
				if err := a.st.DeleteCard(c.ID); err != nil {
					return err
				}
				fmt.Printf("deleted #%d %s\n", c.ID, c.Title)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")
	return cmd
}

func (a *app) boardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "board",
		Aliases: []string{"boards"},
		Short:   "List, add, switch, rename and delete boards",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cur, err := a.st.CurrentBoard()
			if err != nil {
				return err
			}
			boards, err := a.st.Boards()
			if err != nil {
				return err
			}
			for _, b := range boards {
				cards, _ := a.st.Cards(b.ID, false)
				mark := "  "
				name := b.Name
				if b.ID == cur.ID {
					mark = style.AccentFg.Render("▸ ")
					name = style.Bold.Render(name)
				}
				fmt.Printf("%s%s %s\n", mark, name, style.Dim.Render(fmt.Sprintf("· %d cards", len(cards))))
			}
			return nil
		},
	}
	var cols string
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a board",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			names := splitList(cols)
			if len(names) == 0 {
				names = store.DefaultColumns
			}
			b, err := a.st.CreateBoard(strings.Join(args, " "), names)
			if err != nil {
				return err
			}
			if err := a.st.SetCurrentBoard(b.ID); err != nil {
				return err
			}
			fmt.Printf("created %s (%s) and switched to it\n", style.Bold.Render(b.Name), strings.Join(names, ", "))
			return nil
		},
	}
	add.Flags().StringVar(&cols, "cols", "", `columns, comma-separated (default "To do,Doing,Done")`)
	use := &cobra.Command{
		Use:     "use <name>",
		Aliases: []string{"switch"},
		Short:   "Switch the current board",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.st.BoardByName(strings.Join(args, " "))
			if err != nil {
				return err
			}
			if err := a.st.SetCurrentBoard(b.ID); err != nil {
				return err
			}
			fmt.Println("now on " + style.Bold.Render(b.Name))
			return nil
		},
	}
	rename := &cobra.Command{
		Use:   "rename <name> <new name>",
		Short: "Rename a board",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.st.BoardByName(args[0])
			if err != nil {
				return err
			}
			return a.st.RenameBoard(b.ID, args[1])
		},
	}
	var yes bool
	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a board and every card on it",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.st.BoardByName(strings.Join(args, " "))
			if err != nil {
				return err
			}
			cards, _ := a.st.Cards(b.ID, false)
			arch, _ := a.st.Cards(b.ID, true)
			if !yes && !confirm(fmt.Sprintf("delete board %q with %d cards (%d archived)?", b.Name, len(cards)+len(arch), len(arch))) {
				return nil
			}
			if err := a.st.DeleteBoard(b.ID); err != nil {
				return err
			}
			fmt.Println("deleted board " + b.Name)
			return nil
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")
	cmd.AddCommand(add, use, rename, rm)
	return cmd
}

func (a *app) colCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "col",
		Aliases: []string{"cols", "column"},
		Short:   "List, add, rename and delete columns on a board",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			cols, err := a.st.Columns(b.ID)
			if err != nil {
				return err
			}
			cards, _ := a.st.Cards(b.ID, false)
			for _, c := range cols {
				n := 0
				for _, card := range cards {
					if card.ColumnID == c.ID {
						n++
					}
				}
				fmt.Printf("%s %s\n", c.Name, style.Dim.Render(fmt.Sprintf("· %d", n)))
			}
			return nil
		},
	}
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a column at the right end",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			c, err := a.st.CreateColumn(b.ID, strings.Join(args, " "))
			if err != nil {
				return err
			}
			fmt.Printf("added column %s to %s\n", c.Name, b.Name)
			return nil
		},
	}
	rename := &cobra.Command{
		Use:   "rename <name> <new name>",
		Short: "Rename a column",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			c, err := a.st.ColumnByName(b.ID, args[0])
			if err != nil {
				return err
			}
			return a.st.RenameColumn(c.ID, args[1])
		},
	}
	var yes bool
	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a column and its cards",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			c, err := a.st.ColumnByName(b.ID, strings.Join(args, " "))
			if err != nil {
				return err
			}
			n, _ := a.st.CountCards(c.ID)
			if n > 0 && !yes && !confirm(fmt.Sprintf("delete column %q and its %d cards?", c.Name, n)) {
				return nil
			}
			if err := a.st.DeleteColumn(c.ID); err != nil {
				return err
			}
			fmt.Println("deleted column " + c.Name)
			return nil
		},
	}
	rm.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask")
	cmd.AddCommand(add, rename, rm)
	return cmd
}

func (a *app) labelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "label",
		Aliases: []string{"labels", "tag"},
		Short:   "List labels on a board, set their colors or delete them",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			labels, err := a.st.Labels(b.ID)
			if err != nil {
				return err
			}
			if len(labels) == 0 {
				fmt.Println(style.Dim.Render("no labels yet; add one with tack add … -l name or tack label color name blue"))
			}
			for _, l := range labels {
				fmt.Printf("%s %s\n", style.LabelTag(l), style.Dim.Render(l.Color))
			}
			return nil
		},
	}
	color := &cobra.Command{
		Use:   "color <name> <color>",
		Short: "Create a label or change its color (" + strings.Join(store.LabelColors, ", ") + ")",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			l, err := a.st.EnsureLabel(b.ID, args[0], strings.ToLower(args[1]))
			if err != nil {
				return err
			}
			fmt.Println(style.LabelTag(l))
			return nil
		},
	}
	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a label and remove it from every card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := a.board()
			if err != nil {
				return err
			}
			labels, err := a.st.Labels(b.ID)
			if err != nil {
				return err
			}
			for _, l := range labels {
				if strings.EqualFold(l.Name, args[0]) {
					return a.st.DeleteLabel(l.ID)
				}
			}
			return fmt.Errorf("label %q: %w", args[0], store.ErrNotFound)
		},
	}
	cmd.AddCommand(color, rm)
	return cmd
}

func (a *app) checkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "check",
		Aliases: []string{"todo"},
		Short:   "Manage a card's checklist (items are numbered as in tack show)",
	}
	item := func(cardArg, nArg string) (store.ChecklistItem, error) {
		cards, err := a.cards([]string{cardArg})
		if err != nil {
			return store.ChecklistItem{}, err
		}
		n, err := strconv.Atoi(nArg)
		if err != nil || n < 1 || n > len(cards[0].Checklist) {
			return store.ChecklistItem{}, fmt.Errorf("card #%s has no checklist item %s", strings.TrimPrefix(cardArg, "#"), nArg)
		}
		return cards[0].Checklist[n-1], nil
	}
	add := &cobra.Command{
		Use:   "add <card> <text>",
		Short: "Add a checklist item",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cards, err := a.cards(args[:1])
			if err != nil {
				return err
			}
			_, err = a.st.AddChecklistItem(cards[0].ID, strings.Join(args[1:], " "))
			return err
		},
	}
	toggle := &cobra.Command{
		Use:     "toggle <card> <n>",
		Aliases: []string{"x", "tick"},
		Short:   "Tick or untick item n",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			it, err := item(args[0], args[1])
			if err != nil {
				return err
			}
			return a.st.ToggleChecklistItem(it.ID)
		},
	}
	rm := &cobra.Command{
		Use:   "rm <card> <n>",
		Short: "Remove item n",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			it, err := item(args[0], args[1])
			if err != nil {
				return err
			}
			return a.st.DeleteChecklistItem(it.ID)
		},
	}
	cmd.AddCommand(add, toggle, rm)
	return cmd
}

func (a *app) whereCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "where",
		Short: "Print the database path (set TACK_DB to change it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := store.DefaultPath()
			if err != nil {
				return err
			}
			fmt.Println(p)
			return nil
		},
	}
}
