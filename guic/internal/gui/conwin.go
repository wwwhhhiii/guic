package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type ChatsWindow struct {
	SelectedLii widget.ListItemID

	Content       *fyne.Container
	ChatList      *widget.List
	ConnectBtn    *widget.Button
	AddPeerBtn    *widget.Button
	RemoveChatBtn *widget.Button
}

func NewChatsWindow() *ChatsWindow {
	chatList := widget.NewList(nil, nil, nil)
	connectBtn := widget.NewButton("Connect", nil)
	addPeerBtn := widget.NewButton("Add peer", nil)
	controls := container.NewVBox(connectBtn, addPeerBtn)
	removeChatBtn := widget.NewButton("Remove", nil)
	connectionContainer := container.NewBorder(
		container.NewVBox(controls),
		nil, nil, nil,
		container.NewBorder(nil, removeChatBtn, nil, nil, chatList),
	)

	return &ChatsWindow{
		SelectedLii:   -1,
		Content:       connectionContainer,
		ChatList:      chatList,
		ConnectBtn:    connectBtn,
		AddPeerBtn:    addPeerBtn,
		RemoveChatBtn: removeChatBtn,
	}
}

func (cw *ChatsWindow) RemoveChatById(lii widget.ListItemID) {
	fyne.Do(func() {
		cw.ChatList.Unselect(lii)
		cw.ChatList.Refresh()
	})
}
