package app

import _ "embed"

// Tray icons rendered from build/icons/chick-mono.svg (Fluent UI Emoji
// "Baby chick", MIT, mirrored to face right). Regenerate with `make icons`.
var (
	//go:embed icons/tray-template.png
	trayTemplateIcon []byte
	//go:embed icons/tray-light.png
	trayLightIcon []byte
	//go:embed icons/tray-dark.png
	trayDarkIcon []byte
)
