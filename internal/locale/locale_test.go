package locale

import "testing"

func TestFromTag(t *testing.T) {
	for tag, want := range map[string]string{
		"zh-Hans-CN": Zh, "zh_CN.UTF-8": Zh, "zh-TW": Zh, "en-US": En, "ja-JP": En, "": En,
	} {
		if got := fromTag(tag); got != want {
			t.Errorf("fromTag(%q) = %q, want %q", tag, got, want)
		}
	}
	if Resolve("en") != En || Resolve("zh") != Zh {
		t.Fatal("explicit choice ignored")
	}
}
