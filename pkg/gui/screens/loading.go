package screens

import (
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
)

type LoadingScreen struct {
	app      App
	fileName string
	progress float32
}

func NewLoadingScreen(app App) *LoadingScreen {
	return &LoadingScreen{app: app}
}

func (s *LoadingScreen) UpdateProgress(fileName string, i, count int) {
	s.fileName = fileName
	if count > 0 {
		s.progress = float32(i) / float32(count)
	} else {
		s.progress = 0
	}
}

func (s *LoadingScreen) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, layout.Spacer{}.Layout), // top spacer
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Flexed(1, layout.Spacer{}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					w := gtx.Dp(480)
					if w > gtx.Constraints.Max.X {
						w = gtx.Constraints.Max.X
					}
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w

					return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
						// Title
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							title := material.H4(th, s.app.Text("loading_mods", "Loading Mods...", nil))
							title.Color = theme.PrimaryColor
							title.Alignment = text.Middle
							title.Font.Weight = font.Bold
							return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, title.Layout)
						}),
						// Message
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							desc := material.Body1(th, s.app.Text("loading_mods_description", "This should only take a few seconds.", nil))
							desc.Color = theme.TextMutedColor
							desc.Alignment = text.Middle
							return layout.Inset{Bottom: unit.Dp(32)}.Layout(gtx, desc.Layout)
						}),
						// Progress Bar
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							pb := material.ProgressBar(th, s.progress)
							pb.Color = theme.PrimaryColor
							pb.TrackColor = theme.BorderColor
							return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, pb.Layout)
						}),
						// Loading Label
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Body2(th, s.fileName)
							lbl.Color = theme.FgColor
							lbl.Alignment = text.Start
							return lbl.Layout(gtx)
						}),
					)
				}),
				layout.Flexed(1, layout.Spacer{}.Layout),
			)
		}),
		layout.Flexed(1.2, layout.Spacer{}.Layout), // bottom spacer
	)
}
