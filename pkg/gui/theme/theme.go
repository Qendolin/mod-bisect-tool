package theme

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/widget/material"
)

var (
	BgColor           = color.NRGBA{R: 26, G: 29, B: 36, A: 255}
	FgColor           = color.NRGBA{R: 225, G: 228, B: 234, A: 255}
	PrimaryColor      = color.NRGBA{R: 76, G: 120, B: 230, A: 255}
	PrimaryColorHover = color.NRGBA{R: 96, G: 140, B: 250, A: 255}
	SuccessColor      = color.NRGBA{R: 36, G: 100, B: 58, A: 255}
	DangerColor       = color.NRGBA{R: 158, G: 40, B: 46, A: 255}
	WarningColor      = color.NRGBA{R: 255, G: 167, B: 38, A: 255}
	CardBgColor       = color.NRGBA{R: 33, G: 37, B: 48, A: 255}
	BorderColor       = color.NRGBA{R: 45, G: 50, B: 62, A: 255}
	TextMutedColor    = color.NRGBA{R: 128, G: 133, B: 149, A: 255}
)

func NewTheme() *material.Theme {
	th := material.NewTheme()
	th.Palette.Bg = BgColor
	th.Palette.Fg = FgColor
	th.Palette.ContrastBg = PrimaryColor
	th.Palette.ContrastFg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	// Prioritizing Malgun Gothic ensures Korean text uses the real Korean font
	// instead of Microsoft YaHei (msyh.ttc)
	th.Face = font.Typeface(
		"Segoe UI, " +
			"Malgun Gothic, Gulim, " + // Windows Korean
			"Apple SD Gothic Neo, " + // macOS Korean
			"NanumGothic, Noto Sans CJK KR, " + // Linux Korean
			"Microsoft YaHei, " + // Chinese fallback
			"MS Gothic, " + // Japanese fallback
			"sans-serif",
	)

	return th
}
