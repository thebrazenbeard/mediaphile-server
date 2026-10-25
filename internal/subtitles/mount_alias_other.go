//go:build !linux

package subtitles

// Windows and non-Linux platforms receive os.SameFile root/ancestor checks in
// stageOutsideDirectory. Unlike Linux mountinfo, they cannot currently prove
// that two unrelated path roots do not alias a nested media directory.
// Operator qualification must retain the Windows/SMB release gate.
func rejectMountAlias(media, stage string) error { return nil }
