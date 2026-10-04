package main

import (
	"context"
	"flag"
	"fmt"
	"guic/internal"
	"guic/internal/core"
	"guic/internal/gui"
	"guic/internal/widgets"
	"image/color"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"golang.design/x/clipboard"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var debug = flag.Bool("debug", false, "debug mode")

var programLevel = slog.LevelInfo

var ourPeerId = uuid.New()

func main() {
	flag.Parse()
	if *debug {
		programLevel = slog.LevelDebug
	}

	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: programLevel})
	slog.SetDefault(slog.New(h))

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// app
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	var fApp fyne.App
	var mainWindow fyne.Window

	// appCtx := context.Background()
	nickname := core.GenRandNickname()

	// webrtc
	webrtcConf := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	slog.Info("App is running", "name", nickname)

	fApp = app.New()
	mainWindow = fApp.NewWindow("Guic")
	mainWindow.Resize(fyne.NewSize(800, 600))

	chatRegistry := gui.NewChatRegistry()
	chatsWindow := gui.NewChatsWindow()
	displayWindow := gui.NewDisplayWindow()
	guiapp := internal.NewGuicApp(
		fApp,
		mainWindow,
		chatsWindow,
		displayWindow,
		webrtcConf,
		nickname,
		chatRegistry,
	)

	content := container.NewHSplit(
		chatsWindow.Content,
		displayWindow.Content,
	)
	content.SetOffset(0.3)

	setupUI(guiapp)

	go runUIReactor(guiapp)

	// TODO move to setupUI
	mainWindow.SetContent(content)
	mainWindow.SetMaster()
	mainWindow.Show()
	fApp.Run()
}

func runUIReactor(a *internal.GuicApp) {
	for {
		select {
		case peer := <-a.PeerConnected:
			if _, exist := a.ChatRegistry.GetByUUID(peer.Chat.Id); !exist {
				a.ChatRegistry.AddChat(peer.Chat)
			}
			fyne.Do(a.ChatsWindow.ChatList.Refresh)
		case peer := <-a.PeerDisconnected:
			slog.Debug("gui peer disconnected event", "peer", peer.Name, "chatHosted", peer.Chat.IsHosted)
			regEntry, ok := a.ChatRegistry.GetByUUID(peer.Chat.Id)
			if ok && !peer.Chat.IsHosted {
				a.ChatsWindow.RemoveChatById(regEntry.Lii)
				a.DisplayWindow.Clear()
			}
			a.ChatRegistry.RemoveChatByUUID(peer.Chat.Id)
			fyne.Do(func() {
				a.ChatsWindow.ChatList.Refresh()
				a.DisplayWindow.Content.Refresh()
			})
		case msg := <-a.RecvMessage:
			chat, ok := a.ChatRegistry.GetByUUID(msg.ChatId)
			if !ok {
				log.Fatal("chat not found")
			}
			message := fmt.Sprintf("[%s]: %s", msg.PeerName, msg.Text)
			chat.TextScroll.Content.(*fyne.Container).Add(canvas.NewText(message, color.White))
			fyne.Do(chat.TextScroll.Refresh)
		case msg := <-a.CtrlMessage:
			slog.Info("ctrl message", "msg", msg.String, "peer", msg.Peer)
		case msg := <-a.DataMessage:
			slog.Info("data message", "data", msg.Byte)
		}
	}
}

func setupUI(a *internal.GuicApp) {
	a.ChatsWindow.ChatList.Length = func() int { return a.ChatRegistry.Len() }
	a.ChatsWindow.ChatList.CreateItem = func() fyne.CanvasObject {
		return container.NewHBox(widget.NewLabel(""))
	}
	a.ChatsWindow.ChatList.UpdateItem = func(lii widget.ListItemID, co fyne.CanvasObject) {
		chat, ok := a.ChatRegistry.GetByItemId(lii)
		if !ok {
			log.Fatalf("No chat with lii %d found", lii)
		}
		co.(*fyne.Container).Objects[0].(*widget.Label).SetText(chat.Chat.Name)
	}
	a.ChatsWindow.ChatList.OnSelected = func(lii widget.ListItemID) {
		chat, ok := a.ChatRegistry.GetByItemId(lii)
		if !ok {
			log.Fatalf("No chat with lii %d found", lii)
			return
		}
		a.ChatsWindow.SelectedLii = lii
		a.DisplayWindow.SetChat(chat.TextScroll)
	}
	a.ChatsWindow.ChatList.OnUnselected = func(lii widget.ListItemID) {}

	a.ChatsWindow.RemoveChatBtn.OnTapped = func() {
		if a.ChatsWindow.SelectedLii == -1 {
			return
		}
		rmChat := func(remove bool) {
			if !remove {
				return
			}
			chat, ok := a.ChatRegistry.GetByItemId(a.ChatsWindow.SelectedLii)
			if !ok {
				log.Fatalf("chat lii %d not found", a.ChatsWindow.SelectedLii)
			}
			chat.Chat.Close()
			a.ChatRegistry.RemoveChatByUUID(chat.Chat.Id)
			a.ChatsWindow.RemoveChatById(a.ChatsWindow.SelectedLii)
			a.DisplayWindow.Clear()
		}
		dialog.NewConfirm("Confirm", "Remove chat?", rmChat, a.Mainwindow).Show()
	}
	a.ChatsWindow.ConnectBtn.OnTapped = func() {
		connectWin := a.App.NewWindow("Connect to chat")
		connectWin.Resize(fyne.NewSize(400, 200))
		offerEntry := widget.NewMultiLineEntry()
		offerEntry.Wrapping = fyne.TextWrapBreak
		offerEntry.SetPlaceHolder("Paste offer here")

		var submitBtn *widget.Button
		var cancelBtn *widget.Button

		submit := func() {
			activity := widget.NewActivity()
			fyne.Do(func() {
				offerEntry.Disable()
				submitBtn.Disable()
				cancelBtn.Disable()
				activity.Start()
				connectWin.SetContent(
					container.NewBorder(
						widget.NewLabel("Preparing connection..."), nil, nil, nil, activity,
					),
				)
			})

			peer, answer, err := core.SetupAcceptor(
				a.RecvMessage,
				a.PeerDisconnected,
				a.WebrtcConf,
				offerEntry.Text,
				a.Nickname,
			)
			if err != nil {
				widgets.NewModalPopup(fmt.Sprintf("Setup error: %s", err), a.Mainwindow.Canvas()).Show()
				slog.Error("Setup offeree", "error", err)
				return
			}

			sdpstr, err := core.EncodeSDP(answer)
			if err != nil {
				slog.Error("peer answer encode", "error", err)
				widgets.NewModalPopup(fmt.Sprintf("%s", err), a.Mainwindow.Canvas()).Show()
				return
			}

			cpy := func() { clipboard.Write(clipboard.FmtText, []byte(sdpstr)) }
			answerEntry := widget.NewMultiLineEntry()
			answerEntry.Wrapping = fyne.TextWrapBreak
			answerEntry.Disable()
			answerEntry.SetText(sdpstr)
			content := container.NewBorder(
				container.NewVBox(
					widget.NewLabel("Share this with your peer"),
					widget.NewButton("Copy", cpy),
				),
				nil, nil, nil, answerEntry,
			)
			fyne.Do(func() { connectWin.SetContent(content) })

			// wait for peer to send his data
			// TODO cancel should be called by cancel button and cleanup at window close
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
			defer cancel()

			select {
			case <-ctx.Done():
				return
			case <-peer.Ready():
				break
			}
			peer.Chat.AddPeers(peer)
			go peer.Chat.WritePump(context.TODO())
			a.PeerConnected <- peer

			fyne.Do(func() {
				activity.Stop()
				activity.Hide()
				connectWin.SetContent(
					container.NewVBox(
						widget.NewLabel(fmt.Sprintf("Peer %s connected", peer.Name)),
					),
				)
			})
		}

		offerEntry.OnSubmitted = func(s string) { go submit() }
		submitBtn = widget.NewButton("Submit", func() { go submit() })
		cancelBtn = widget.NewButton("Cancel", func() { connectWin.Close() })
		content := container.NewBorder(
			nil, container.NewVBox(submitBtn, cancelBtn), nil, nil, offerEntry,
		)
		connectWin.SetContent(content)
		connectWin.Show()
	}
	a.ChatsWindow.AddPeerBtn.OnTapped = func() {
		addWin := a.App.NewWindow("Add new peer")
		addWin.Resize(fyne.NewSize(400, 200))

		entries := a.ChatRegistry.List()
		chatNames := make([]string, 0, len(entries))
		for _, e := range entries {
			chatNames = append(chatNames, e.Chat.Name)
		}
		chatsSelect := widget.NewSelect(chatNames, func(s string) {})
		chatsSelect.PlaceHolder = "Create new chat"

		submit := func() {
			var chat *core.Chat
			if chatsSelect.SelectedIndex() == -1 {
				prompt, promptSubmitted := internal.TextPrompt(addWin.Canvas(), "Enter new chat name")
				addWin.Canvas().Focus(prompt)
				name := <-promptSubmitted
				chat = core.CreateChat(name, true)
			} else {
				chat = entries[chatsSelect.SelectedIndex()].Chat
				if !chat.IsHosted {
					widgets.NewModalPopup("You are not chat host", addWin.Canvas()).Show()
					return
				}
			}

			fyne.Do(func() {
				activity := widget.NewActivity()
				activity.Start()
				addWin.SetContent(
					container.NewBorder(
						widget.NewLabel("Contacting STUN servers..."), nil, nil, nil, activity),
				)
			})

			offerstr, peer, err := core.SetupInitiator(
				a.RecvMessage,
				a.PeerDisconnected,
				a.WebrtcConf,
				chat,
				a.Nickname,
			)
			if err != nil {
				log.Fatalln(err)
			}

			offerentry := widget.NewMultiLineEntry()
			offerentry.SetText(offerstr)
			offerentry.Disable()
			offerentry.Wrapping = fyne.TextWrapBreak
			answerentry := widget.NewMultiLineEntry()
			answerentry.Wrapping = fyne.TextWrapBreak

			activity := widget.NewActivity()
			onPressFinish := func() {
				fyne.Do(func() {
					activity.Start()
					activity.Show()
					addWin.SetContent(
						container.NewBorder(
							widget.NewLabel("Finishing connection setup..."), nil, nil, nil, activity,
						),
					)
				})

				sdp, err := core.DecodeSDP(answerentry.Text)
				if err != nil {
					fyne.Do(func() { addWin.SetContent(widget.NewLabel(fmt.Sprintf("Setup error: %s", err))) })
					return
				}
				if err := peer.Conn.SetRemoteDescription(*sdp); err != nil {
					fyne.Do(func() { addWin.SetContent(widget.NewLabel(fmt.Sprintf("Setup error: %s", err))) })
					return
				}

				// TODO
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
				defer cancel()

				select {
				case <-ctx.Done():
					fyne.Do(func() { addWin.SetContent(widget.NewLabel(fmt.Sprintf("Setup error: %s", ctx.Err()))) })
					return
				case <-peer.Ready():
					break
				}

				chat.AddPeers(peer)
				go chat.WritePump(context.TODO())
				a.PeerConnected <- peer

				fyne.Do(func() {
					activity.Stop()
					activity.Hide()
					addWin.SetContent(
						container.NewVBox(
							widget.NewLabel(fmt.Sprintf("Peer %s is connected", peer.Name)),
						),
					)
				})
			}

			content := container.NewBorder(
				container.NewVBox(
					widget.NewLabel("Share this with your peer"),
					widget.NewButton("Copy", func() { clipboard.Write(clipboard.FmtText, []byte(offerstr)) }),
				),
				container.NewVBox(
					widget.NewButton("Finish", func() { go onPressFinish() }),
					widget.NewButton("Cancel", func() {}),
				),
				nil,
				nil,
				container.NewVBox(
					offerentry,
					widget.NewLabel("Put peer answer here"),
					answerentry,
				),
			)
			fyne.Do(func() { addWin.SetContent(content) })
		}

		content := container.NewBorder(
			widget.NewLabel("Select chat to add peer to"),
			container.NewVBox(
				widget.NewButton("Next", func() { go submit() }),
			),
			nil,
			nil,
			chatsSelect,
		)
		addWin.SetContent(content)
		addWin.SetOnClosed(func() {
			// TODO add cleanup logic
		})
		addWin.Show()
	}

	onSend := func(text string) {
		if text == "" || a.ChatsWindow.SelectedLii == -1 {
			return
		}
		e, ok := a.ChatRegistry.GetByItemId(a.ChatsWindow.SelectedLii)
		if !ok {
			log.Fatalf("chat lii %d not found", a.ChatsWindow.SelectedLii)
		}
		scroll := e.TextScroll.Content.(*fyne.Container)
		message := fmt.Sprintf("[%s]: %s", a.Nickname, text)
		scroll.Add(canvas.NewText(message, color.White))
		a.DisplayWindow.TextEntry.SetText("")
		e.Chat.SendMessage(&core.Message{
			PeerName: a.Nickname,
			PeerId:   ourPeerId,
			ChatId:   e.Chat.Id,
			Text:     text,
		})

	}

	a.DisplayWindow.SendBtn.OnTapped = func() { onSend(a.DisplayWindow.TextEntry.Text) }
	a.DisplayWindow.TextEntry.OnSubmitted = onSend
}
