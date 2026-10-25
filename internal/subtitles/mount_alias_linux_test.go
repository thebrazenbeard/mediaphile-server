//go:build linux

package subtitles

import "testing"

func TestLinuxMountAliasRejectsBindOfLibraryDescendant(t *testing.T) {
	const info = "1 0 8:1 / / rw - ext4 /dev/sda1 rw\n" +
		"2 1 8:1 /srv/media/Travelers /private-stage rw - ext4 /dev/sda1 rw\n"
	if err := rejectMountAliasFromInfo("/srv/media", "/private-stage", info); err == nil {
		t.Fatal("bind alias of a library descendant was accepted")
	}
	if err := rejectMountAliasFromInfo("/srv/media", "/safe-stage", info); err != nil {
		t.Fatalf("independent sibling was rejected: %v", err)
	}
}

func TestLinuxMountAliasMountInfoFailureIsClosed(t *testing.T) {
	if err := rejectMountAliasFromInfo("/srv/media", "/private-stage", "malformed input"); err == nil {
		t.Fatal("unresolved mount identities were accepted")
	}
}

func TestLinuxMountAliasRejectsNestedBindMountAndEscapes(t *testing.T) {
	const info = "1 0 8:1 / / rw - ext4 /dev/sda1 rw\n" +
		"5 1 8:1 /srv/media/TV\\040Shows/Travelers /stage\\040space rw - ext4 /dev/sda1 rw\n"
	if err := rejectMountAliasFromInfo("/srv/media/TV Shows", "/stage space", info); err == nil {
		t.Fatal("space-escaped bind alias was accepted")
	}
}
