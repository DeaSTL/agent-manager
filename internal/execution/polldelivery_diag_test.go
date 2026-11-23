package execution

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Temporary diagnostic for the slow-submit flake: samples the caret and the
// composer read while the tool redraws its spinner after taking the Enter.
func TestDiagSlowSubmitCaretDuringSpinner(t *testing.T) {
	var report []string
	for iter := 0; iter < 8; iter++ {
		m := buildModel(t)
		sess := spawnedSession(t, m, "slow-submit-tool")
		settledPane(t, m, sess.ID, "❯")
		if err := m.tmux.Paste(sess.ID, "rebase on main\n\nthen run the suite"); err != nil {
			t.Fatal(err)
		}
		waitFor := time.Now().Add(10 * time.Second)
		for {
			pane, _ := m.tmux.CapturePane(sess.ID)
			if strings.Contains(pane, "Pasted text") || time.Now().After(waitFor) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := m.tmux.SendKeys(sess.ID, "Enter"); err != nil {
			t.Fatal(err)
		}
		drafted := m.poller.composerDrafted(sess)
		counts := map[string]int{}
		var firstBad []string
		var slowest time.Duration
		end := time.Now().Add(2500 * time.Millisecond)
		for time.Now().Before(end) {
			xy, pane, err := m.tmux.DisplayAndCapture(sess.ID, "#{cursor_x},#{cursor_y}")
			if err != nil {
				counts["atomic-err"]++
				continue
			}
			rows := strings.Split(ansi.Strip(pane), "\n")
			composer, fresh := -1, false
			for i, row := range rows {
				if strings.HasPrefix(row, "❯ [Pasted") {
					composer = i
				}
				if strings.TrimSpace(row) == "❯" {
					fresh = true
				}
			}
			_, ys, _ := strings.Cut(strings.TrimSpace(xy), ",")
			caretY, _ := strconv.Atoi(ys)
			if !fresh {
				counts["atomic-samples"]++
				if caretY != composer {
					counts["atomic-caret-off-composer"]++
					if len(firstBad) < 3 {
						firstBad = append(firstBad, fmt.Sprintf("atomic caret=%s pane=%q", xy, firstRows(rows)))
					}
				}
			}
			started := time.Now()
			typed, derr := drafted()
			if took := time.Since(started); took > slowest {
				slowest = took
			}
			_, after, _ := m.tmux.DisplayAndCapture(sess.ID, "")
			freshAfter := false
			for _, row := range strings.Split(ansi.Strip(after), "\n") {
				if strings.TrimSpace(row) == "❯" {
					freshAfter = true
				}
			}
			switch {
			case derr != nil:
				counts["drafted-err"]++
			case !typed && !freshAfter:
				counts["drafted-false-before-fresh-prompt"]++
				if len(firstBad) < 6 {
					firstBad = append(firstBad, fmt.Sprintf("drafted false, after=%q", firstRows(strings.Split(ansi.Strip(after), "\n"))))
				}
			case !freshAfter:
				counts["drafted-true-before-fresh-prompt"]++
			}
		}
		report = append(report, fmt.Sprintf("iter %d: %v slowest-drafted=%v bad=%q", iter, counts, slowest, firstBad))
	}
	t.Errorf("DIAG\n%s", strings.Join(report, "\n"))
}

func firstRows(rows []string) string {
	return strings.Join(rows[:min(4, len(rows))], "|")
}
