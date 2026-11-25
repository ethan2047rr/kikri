package locale

import "golang.org/x/sys/windows"

// systemTag is the Windows display language, e.g. "zh-CN".
func systemTag() string {
	if langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(langs) > 0 {
		return langs[0]
	}
	return envTag()
}
