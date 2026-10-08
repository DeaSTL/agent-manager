package sessioncmd

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// A Unix socket path stops near 104 bytes, past which t.TempDir's names run.
func focusSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "amfocus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "agentmgr.focus")
}

func askFocus(t *testing.T, path string, request FocusRequest) <-chan FocusAnswer {
	t.Helper()
	answers := make(chan FocusAnswer, 1)
	go func() {
		var answer FocusAnswer
		conn, err := net.Dial("unix", path)
		if err != nil {
			answer.Error = "dial: " + err.Error()
			answers <- answer
			return
		}
		defer conn.Close()
		if err := json.NewEncoder(conn).Encode(request); err != nil {
			answer.Error = "send: " + err.Error()
		} else if err := json.NewDecoder(conn).Decode(&answer); err != nil {
			answer.Error = "read: " + err.Error()
		}
		answers <- answer
	}()
	return answers
}

// Each connection carries one request and reads back the outcome the
// manager handed back for that request, a refusal included.
func TestFocusEndpointAnswersEachRequestWithItsOutcome(t *testing.T) {
	path := focusSocketPath(t)
	deliveries := make(chan FocusDelivery, 1)
	stop := ListenFocus(path, func(delivery FocusDelivery) { deliveries <- delivery })
	defer stop()

	answers := askFocus(t, path, FocusRequest{ID: "a", Enter: true})
	delivery := <-deliveries
	if delivery.Request != (FocusRequest{ID: "a", Enter: true}) {
		t.Fatalf("delivered %+v, want a entered", delivery.Request)
	}
	delivery.Answer <- nil
	if answer := <-answers; answer.Error != "" {
		t.Fatalf("answered %q, want success", answer.Error)
	}

	answers = askFocus(t, path, FocusRequest{ID: "b"})
	delivery = <-deliveries
	delivery.Answer <- os.ErrNotExist
	if answer := <-answers; answer.Error != os.ErrNotExist.Error() {
		t.Fatalf("answered %q, want the refusal", answer.Error)
	}
}

// One manager answers per tmux server: a second one leaves a live endpoint
// to its owner, and a socket left by a manager that exited is taken over.
func TestFocusEndpointKeepsALiveOwnerAndReplacesAStaleSocket(t *testing.T) {
	path := focusSocketPath(t)
	first := make(chan FocusDelivery, 1)
	stopFirst := ListenFocus(path, func(delivery FocusDelivery) { first <- delivery })
	second := make(chan FocusDelivery, 1)
	stopSecond := ListenFocus(path, func(delivery FocusDelivery) { second <- delivery })
	defer stopSecond()
	askFocus(t, path, FocusRequest{ID: "a"})
	(<-first).Answer <- nil
	if len(second) != 0 {
		t.Fatal("the second manager took a request from the live endpoint")
	}
	stopFirst()

	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	third := make(chan FocusDelivery, 1)
	stopThird := ListenFocus(path, func(delivery FocusDelivery) { third <- delivery })
	defer stopThird()
	askFocus(t, path, FocusRequest{ID: "b"})
	if delivery := <-third; delivery.Request.ID != "b" {
		t.Fatalf("request = %+v, want b", delivery.Request)
	}
}
