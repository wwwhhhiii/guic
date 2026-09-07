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

	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: programLevel})
	slog.SetDefault(slog.New(h))

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// app
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	var application fyne.App
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

	application = app.New()
	mainWindow = application.NewWindow("Guic")
	mainWindow.Resize(fyne.NewSize(800, 600))

	chatRegistry := gui.NewChatRegistry()
	connectionWindow := gui.NewConnectionWindow()
	chatWindow := gui.NewChatWindow()
	guiapp := internal.NewGuicApp(application, mainWindow, webrtcConf, nickname, chatRegistry)

	content := container.NewHSplit(
		connectionWindow.Content,
		chatWindow.Content,
	)
	content.SetOffset(0.3)

	setupUI(connectionWindow, chatWindow, guiapp)

	// UI reactor
	go func() {
		for {
			select {
			case peer := <-guiapp.PeerConnected:
				chatRegistry.AddChat(peer.Chat)
				fyne.Do(connectionWindow.ChatList.Refresh)
			case peer := <-guiapp.PeerDisconnected:
				chatRegistry.RemoveChatByUUID(peer.Chat.Id)
				fyne.Do(connectionWindow.ChatList.Refresh)
			case msg := <-guiapp.RecvMessage:
				chat, ok := chatRegistry.GetByUUID(msg.ChatId)
				if !ok {
					log.Fatal("chat not found")
				}
				message := fmt.Sprintf("[%s]: %s", msg.PeerName, msg.Text)
				chat.TextScroll.Content.(*fyne.Container).Add(canvas.NewText(message, color.White))
				fyne.Do(chat.TextScroll.Refresh)
			case msg := <-guiapp.CtrlMessage:
				slog.Info("ctrl message", "msg", msg.String, "peer", msg.Peer)
			case msg := <-guiapp.DataMessage:
				slog.Info("data message", "data", msg.Byte)
			}
		}
	}()

	mainWindow.SetContent(content)
	mainWindow.SetMaster()
	mainWindow.Show()
	application.Run()
}

func setupUI(w *gui.ConnectionWindow, chw *gui.ChatWindow, guiapp *internal.GuicApp) {
	w.ChatList.Length = func() int { return guiapp.ChatRegistry.Len() }
	w.ChatList.CreateItem = func() fyne.CanvasObject {
		return container.NewHBox(widget.NewLabel(""))
	}
	w.ChatList.UpdateItem = func(lii widget.ListItemID, co fyne.CanvasObject) {
		chat, ok := guiapp.ChatRegistry.GetByItemId(lii)
		if !ok {
			log.Fatal("No chat found")
		}
		co.(*fyne.Container).Objects[0].(*widget.Label).SetText(chat.Chat.Name)
	}
	w.ChatList.OnSelected = func(lii widget.ListItemID) {
		chat, ok := guiapp.ChatRegistry.GetByItemId(lii)
		if !ok {
			log.Fatal("No chat found")
			return
		}
		w.SelectedLii = lii
		chw.SetChat(chat.TextScroll)
	}
	w.ChatList.OnUnselected = func(lii widget.ListItemID) {}

	w.RemoveChatBtn.OnTapped = func() {
		// TODO
		rmChat := func(remove bool) {
			if !remove {
				return
			}
		}
		dialog.NewConfirm("Confirm", "Remove chat?", rmChat, guiapp.Mainwindow).Show()
	}
	w.ConnectBtn.OnTapped = func() {
		connectWin := guiapp.App.NewWindow("Connect to chat")
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

			peer, answer, err := core.SetupOfferee(
				guiapp.RecvMessage,
				guiapp.PeerDisconnected,
				guiapp.WebrtcConf,
				offerEntry.Text,
				guiapp.Nickname,
			)
			if err != nil {
				widgets.NewModalPopup(fmt.Sprintf("Setup error: %s", err), guiapp.Mainwindow.Canvas()).Show()
				slog.Error("Setup offeree", "error", err)
				return
			}

			sdpstr, err := core.EncodeSDP(answer)
			if err != nil {
				slog.Error("peer answer encode", "error", err)
				widgets.NewModalPopup(fmt.Sprintf("%s", err), guiapp.Mainwindow.Canvas()).Show()
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
				// TODO
				return
			case <-peer.Ready():
				break
			}
			peer.Chat.AddPeers(peer)
			go peer.Chat.WritePump(context.TODO())
			guiapp.PeerConnected <- peer

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
	w.AddPeerBtn.OnTapped = func() {
		addWin := guiapp.App.NewWindow("Add new peer")
		addWin.Resize(fyne.NewSize(400, 200))

		chatsNames := make([]string, 0, 10)
		chatsSelect := widget.NewSelect(chatsNames, func(s string) {})
		chatsSelect.PlaceHolder = "Create new chat"

		submit := func() {
			var chat *core.Chat
			if chatsSelect.SelectedIndex() == -1 {
				prompt, promptSubmitted := internal.TextPrompt(addWin.Canvas(), "Enter new chat name")
				addWin.Canvas().Focus(prompt)
				name := <-promptSubmitted
				chat = core.CreateChat(name, true)
			} else {
				id, err := uuid.Parse(chatsSelect.Selected)
				if err != nil {
					log.Fatalln(err)
				}
				var exist bool
				chat, exist = core.GetChat(id)
				if !exist {
					widgets.NewModalPopup("Error. Selected chat does not exist", addWin.Canvas()).Show()
					return
				}
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

			offerstr, peer, err := core.SetupOfferor(
				guiapp.RecvMessage,
				guiapp.WebrtcConf,
				chat,
				guiapp.Nickname,
				chat.Name,
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
				guiapp.PeerConnected <- peer

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
		// TODO do a cleanup logic
		addWin.SetOnClosed(func() {})
		addWin.Show()
	}

	chw.SendBtn.OnTapped = func() {
		e, ok := guiapp.ChatRegistry.GetByItemId(w.SelectedLii)
		if !ok {
			log.Fatal("chat not found")
		}
		e.Chat.SendMessage(&core.Message{
			PeerName: guiapp.Nickname,
			PeerId:   ourPeerId,
			ChatId:   e.Chat.Id,
			Text:     chw.TextEntry.Text,
		})
	}
}
