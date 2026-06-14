package pathcheck

import (
	"fmt"
	"path"
	"strings"
	"unicode"

	"braid/internal/mirror"
)

type Error struct {
	Path   string
	Reason string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return "invalid path: " + e.Reason
	}
	return fmt.Sprintf("invalid path %q: %s", e.Path, e.Reason)
}

func ValidateLocal(localPath string, existing []string) error {
	if err := validatePortable(localPath, true); err != nil {
		return err
	}
	clean := cleanSlash(localPath)
	for _, existingPath := range existing {
		if strings.EqualFold(clean, cleanSlash(existingPath)) {
			return &Error{Path: localPath, Reason: "case-fold collision with existing mirror path " + existingPath}
		}
	}
	return nil
}

func ValidateUpstream(upstreamPath string) error {
	return validatePortable(upstreamPath, false)
}

func CheckRemoteCollision(candidate mirror.Mirror, existing []mirror.Mirror) error {
	candidateRemote := candidate.Remote()
	for _, existingMirror := range existing {
		if candidateRemote == existingMirror.Remote() {
			return fmt.Errorf("remote name collision: %q for mirror paths %q and %q", candidateRemote, candidate.Path, existingMirror.Path)
		}
	}
	return nil
}

func validatePortable(value string, local bool) error {
	if value == "" {
		return &Error{Reason: "empty path"}
	}
	if value == "." {
		return &Error{Path: value, Reason: "dot path"}
	}
	if strings.Contains(value, `\`) {
		return &Error{Path: value, Reason: "backslash separators are not portable"}
	}
	if strings.HasPrefix(value, "//") {
		return &Error{Path: value, Reason: "UNC paths are not allowed"}
	}
	if isWindowsDrivePath(value) {
		return &Error{Path: value, Reason: "Windows drive paths are not allowed"}
	}
	if path.IsAbs(value) {
		return &Error{Path: value, Reason: "absolute paths are not allowed"}
	}

	elements := strings.Split(value, "/")
	for _, element := range elements {
		if element == "" {
			return &Error{Path: value, Reason: "empty path element"}
		}
		if element == "." {
			return &Error{Path: value, Reason: "dot path element"}
		}
		if element == ".." {
			return &Error{Path: value, Reason: "parent traversal is not allowed"}
		}
		if strings.HasSuffix(element, " ") || strings.HasSuffix(element, ".") {
			return &Error{Path: value, Reason: "path elements must not end with space or dot"}
		}
		if strings.Contains(element, ":") {
			return &Error{Path: value, Reason: "colon is not allowed in path elements"}
		}
		if local {
			if strings.EqualFold(element, ".git") {
				return &Error{Path: value, Reason: "local mirror path must not be under .git"}
			}
			if isWindowsReservedBase(element) {
				return &Error{Path: value, Reason: "Windows reserved basename is not allowed"}
			}
		}
	}
	return nil
}

func cleanSlash(value string) string {
	return strings.TrimRight(value, "/")
}

func isWindowsDrivePath(value string) bool {
	return len(value) >= 2 && isASCIIAlpha(rune(value[0])) && value[1] == ':'
}

func isASCIIAlpha(r rune) bool {
	return r <= unicode.MaxASCII && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
}

func isWindowsReservedBase(element string) bool {
	base := element
	if idx := strings.IndexByte(base, '.'); idx >= 0 {
		base = base[:idx]
	}
	base = strings.ToUpper(base)
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}
