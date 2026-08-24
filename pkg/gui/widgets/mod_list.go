package widgets

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/i18n"
	"github.com/Qendolin/mod-bisect-tool/pkg/gui/theme"
)

// ModListTag marks how a mod row should be styled relative to a plain entry.
type ModListTag string

const (
	ModListTagNone          ModListTag = ""
	ModListTagDependency    ModListTag = "dependency"
	ModListTagAlwaysEnabled ModListTag = "always"
)

// ModListItem is a single row in a ModList: a friendly name with the mod ID
// below, mirroring the mod selection screen.
type ModListItem struct {
	Name string
	ID   string
	// Tag styles the row. Rows tagged with ModListTagDependency render the name
	// muted and italic; the tag itself is appended to the ID line as a hint.
	Tag ModListTag
}

// ModList is a scrollable list of mod items rendered with a card background,
// a rounded border, and 1dp separator lines between rows.
type ModList struct {
	list widget.List
	// selections holds one Selectable per rendered row. It is intentionally
	// never trimmed: keeping it monotonic (only growing) keeps the layout logic
	// simple, and indices are always bounded by len(items) at render time.
	selections []*widget.Selectable
}

func NewModList() *ModList {
	ml := &ModList{}
	ml.list.Axis = layout.Vertical
	return ml
}

func (ml *ModList) Layout(gtx layout.Context, th *material.Theme, items []ModListItem, translator *i18n.Translator) layout.Dimensions {
	for len(ml.selections) < len(items) {
		ml.selections = append(ml.selections, new(widget.Selectable))
	}

	return layout.Stack{}.Layout(gtx,
		// Background fill + border drawn over the full allocated size.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, theme.CardBgColor, clip.Rect{Max: gtx.Constraints.Max}.Op())
			return widget.Border{
				Color:        theme.BorderColor,
				CornerRadius: unit.Dp(4),
				Width:        unit.Dp(1),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		// Scrollable rows stacked on top.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.List(th, &ml.list).Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
					item := items[i]
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							name := item.Name
							if name == "" {
								name = item.ID
							}
							lbl := material.Body2(th, name)
							lbl.State = ml.selections[i]
							lbl.SelectionColor = theme.PrimaryColor
							lbl.Color = theme.FgColor
							lbl.MaxLines = 1
							if item.Tag == ModListTagDependency {
								lbl.Color = theme.TextMutedColor
								lbl.Font.Style = font.Italic
							}
							return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, lbl.Layout)
						}),
						// ID line with an optional tag hint.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							idText := item.ID
							if item.Tag != ModListTagNone {
								idText += " (" + translator.Text("mod_tag_"+string(item.Tag), string(item.Tag), nil) + ")"
							}
							lbl := material.Body2(th, idText)
							lbl.Color = theme.TextMutedColor
							lbl.MaxLines = 1
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, lbl.Layout)
						}),
						// Separator, omitted on the last item.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if i == len(items)-1 {
								return layout.Dimensions{}
							}
							sz := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(1)}
							paint.FillShape(gtx.Ops, theme.BorderColor, clip.Rect{Max: sz}.Op())
							return layout.Dimensions{Size: sz}
						}),
					)
				})
			})
		}),
	)
}
