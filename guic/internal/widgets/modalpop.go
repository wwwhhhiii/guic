package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func NewModalPopup(message string, canvas fyne.Canvas) *widget.PopUp {
	var modal *widget.PopUp
	closeBtn := widget.NewButton("Close", func() {
		modal.Hide()
	})
	popupContent := container.NewVBox(
		widget.NewLabel(message),
		closeBtn,
	)
	modal = widget.NewModalPopUp(
		popupContent,
		canvas,
	)
	return modal
}
