package locale

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// systemTag is the first of the user's preferred languages (System Settings
// → General → Language & Region). A GUI app started by launchd has no LANG.
func systemTag() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err == nil {
		// (\n    "zh-Hans-CN",\n    "en-CN"\n)
		for _, line := range strings.Split(string(out), "\n") {
			if t := strings.Trim(strings.TrimSpace(line), `",()`); t != "" {
				return t
			}
		}
	}
	return envTag()
}
