package internal

import (
	"guic/internal/core"
	"guic/internal/gui"

	"fyne.io/fyne/v2"
	"github.com/pion/webrtc/v4"
)

type GuicApp struct {
	App           fyne.App
	Mainwindow    fyne.Window
	ChatsWindow   *gui.ChatsWindow
	DisplayWindow *gui.DisplayWindow

	ChatRegistry *gui.ChatRegistry
	WebrtcConf   webrtc.Configuration
	Nickname     string

	PeerConnected    chan *core.Peer
	PeerDisconnected chan *core.Peer
	RecvMessage      chan *core.Message
	CtrlMessage      chan struct {
		String string
		Peer   *core.Peer
	}
	DataMessage chan struct {
		Byte byte
		Peer *core.Peer
	}
}

func NewGuicApp(
	app fyne.App,
	mainWindow fyne.Window,
	connWindow *gui.ChatsWindow,
	displayWindow *gui.DisplayWindow,
	webrtcConf webrtc.Configuration,
	nickname string,
	chatRegistry *gui.ChatRegistry,
) *GuicApp {
	return &GuicApp{
		App:              app,
		ChatsWindow:      connWindow,
		DisplayWindow:    displayWindow,
		Mainwindow:       mainWindow,
		WebrtcConf:       webrtcConf,
		Nickname:         nickname,
		ChatRegistry:     chatRegistry,
		PeerConnected:    make(chan *core.Peer, 20),
		PeerDisconnected: make(chan *core.Peer, 20),
		RecvMessage:      make(chan *core.Message, 100),
		CtrlMessage: make(chan struct {
			String string
			Peer   *core.Peer
		}, 10),
		DataMessage: make(chan struct {
			Byte byte
			Peer *core.Peer
		}, 10),
	}
}
