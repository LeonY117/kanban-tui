package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestAddWarnsOnFragileTitleEmoji(t *testing.T) {
	sandboxTicket(t)
	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)

	rootCmd.SetArgs([]string{"add", "🗄️ Slice 2 — Redis + worker split"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(errOut.String(), "🗄️") || !strings.Contains(errOut.String(), "note:") {
		t.Errorf("stderr should carry a note naming the emoji, got: %q", errOut.String())
	}
}

func TestAddStaysSilentOnSafeEmoji(t *testing.T) {
	sandboxTicket(t)
	var errOut bytes.Buffer
	rootCmd.SetErr(&errOut)

	rootCmd.SetArgs([]string{"add", "🔒 Review agent tool permissions"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("safe emoji should produce no note, got: %q", errOut.String())
	}
}

func TestUpdateWarnsOnlyWhenTitleChanges(t *testing.T) {
	_, ticket := sandboxTicket(t)
	var errOut bytes.Buffer
	rootCmd.SetErr(&errOut)

	rootCmd.SetArgs([]string{"update", ticket.ShortID, "--status", "doing"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update status: %v", err)
	}
	if errOut.Len() != 0 {
		t.Errorf("no title change should mean no note, got: %q", errOut.String())
	}

	rootCmd.SetArgs([]string{"update", ticket.ShortID, "--title", "✏️ retitled"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update title: %v", err)
	}
	if !strings.Contains(errOut.String(), "✏️") {
		t.Errorf("fragile retitle should produce a note, got: %q", errOut.String())
	}
}
