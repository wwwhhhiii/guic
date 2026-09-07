package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type ChatWindow struct {
	Content   *fyne.Container
	TextEntry *widget.Entry
	SendBtn   *widget.Button
}

func NewChatWindow() *ChatWindow {
	textEntry := widget.NewEntry()
	textEntry.SetPlaceHolder("Enter a message")
	sendBtn := widget.NewButton("Send", nil)
	placeholderScroll := container.NewVScroll(container.NewVBox())
	sendEntry := container.NewVBox(
		textEntry,
		container.NewBorder(nil, nil, nil, nil, sendBtn),
	)
	content := container.NewBorder(nil, sendEntry, nil, nil, placeholderScroll)

	return &ChatWindow{
		Content:   content,
		TextEntry: textEntry,
		SendBtn:   sendBtn,
	}
}

func (cw *ChatWindow) Enable() {
	cw.TextEntry.Enable()
	cw.SendBtn.Enable()
}

func (cw *ChatWindow) Disable() {
	cw.TextEntry.Disable()
	cw.SendBtn.Disable()
}

func (cw *ChatWindow) CurrentScroll() *container.Scroll {
	return cw.Content.Objects[0].(*container.Scroll)
}

func (cw *ChatWindow) SetChat(win fyne.CanvasObject) {
	scroll := win.(*container.Scroll)
	fyne.Do(func() {
		cw.CurrentScroll().Hide()
		// TODO unsafe, need to determine scroll exactly
		cw.Content.Objects[0] = scroll
		cw.CurrentScroll().Show()
		cw.Content.Refresh()
	})
}
