//go:build linux

package subtitles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Linux's mountinfo exposes the filesystem device and the backing root of a
// bind mount. Merely resolving symlinks cannot reveal that /stage aliases
// /media/season through a bind mount. Fail closed if this map is unavailable.
type mountIdentity struct {
	device string
	root   string
	point  string
}

func rejectMountAlias(media, stage string) error {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return errors.New("cannot verify staging mount identity")
	}
	return rejectMountAliasFromInfo(media, stage, string(data))
}

func rejectMountAliasFromInfo(media, stage, contents string) error {
	mounts := parseMountInfo(contents)
	a, okA := mountBacking(media, mounts)
	b, okB := mountBacking(stage, mounts)
	if !okA || !okB {
		return errors.New("cannot resolve staging mount identity")
	}
	if a.device == b.device && (pathWithin(a.root, b.root) || pathWithin(b.root, a.root)) {
		return errors.New("staging and media library overlap through mount aliases")
	}
	return nil
}

func parseMountInfo(contents string) []mountIdentity {
	mounts := []mountIdentity{}
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[0] == "" || fields[4] == "" {
			continue
		}
		foundSep := false
		for _, f := range fields[6:] {
			if f == "-" {
				foundSep = true
				break
			}
		}
		if !foundSep {
			continue
		}
		mounts = append(mounts, mountIdentity{
			device: fields[2], root: decodeMountField(fields[3]), point: decodeMountField(fields[4]),
		})
	}
	return mounts
}

func decodeMountField(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func pathWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func mountBacking(path string, mounts []mountIdentity) (mountIdentity, bool) {
	var best mountIdentity
	longest := -1
	for _, mount := range mounts {
		// Equal mount points can be stacked. The later visible mount replaces
		// the earlier identity; choosing the first can hide a bind alias.
		if !pathWithin(mount.point, path) || len(mount.point) < longest {
			continue
		}
		rel, err := filepath.Rel(mount.point, path)
		if err != nil {
			continue
		}
		best = mountIdentity{device: mount.device, root: filepath.Join(mount.root, rel), point: mount.point}
		longest = len(mount.point)
	}
	return best, longest >= 0
}
