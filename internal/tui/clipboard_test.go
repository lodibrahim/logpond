package tui

import "testing"

func TestClipboardCmdDarwin(t *testing.T) {
	name, args := clipboardCmdForOS("darwin")
	if name != "pbcopy" {
		t.Errorf("name = %q, want %q", name, "pbcopy")
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestClipboardCmdLinux(t *testing.T) {
	name, args := clipboardCmdForOS("linux")
	// On this machine xclip/xsel may or may not exist.
	// Either result is valid — just check the contract.
	switch name {
	case "xclip":
		if len(args) != 2 || args[0] != "-selection" || args[1] != "clipboard" {
			t.Errorf("xclip args = %v, want [-selection clipboard]", args)
		}
	case "xsel":
		if len(args) != 2 || args[0] != "--clipboard" || args[1] != "--input" {
			t.Errorf("xsel args = %v, want [--clipboard --input]", args)
		}
	default:
		t.Errorf("unexpected clipboard command: %q", name)
	}
}
