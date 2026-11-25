// Package locale picks Kikri's UI language: Chinese or English.
package locale

import (
	"os"
	"strings"
)

// The supported UI languages.
const (
	Zh = "zh"
	En = "en"
)

// Resolve returns the language to use: pref when it is "zh" or "en",
// otherwise ("" = follow the system) the system's.
func Resolve(pref string) string {
	if pref == Zh || pref == En {
		return pref
	}
	return System()
}

// System returns Zh when the system's preferred language is Chinese and En
// otherwise.
func System() string {
	return fromTag(systemTag())
}

// fromTag maps a language tag ("zh-Hans-CN", "zh_CN.UTF-8", "en-US") to Zh/En.
func fromTag(tag string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tag)), "zh") {
		return Zh
	}
	return En
}

// envTag reads the POSIX locale variables, used where the OS has no better
// source.
func envTag() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			return v
		}
	}
	return ""
}
