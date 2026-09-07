package gui

import (
	"guic/internal/core"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/google/uuid"
)

type Application struct {
	app              *fyne.App
	mainWin          *fyne.Window
	connectionWindow *ConnectionWindow
	chatWindow       *ChatWindow
}

func NewApplication(connectionWindow *ConnectionWindow, chatWindow *ChatWindow) *Application {
	a := app.New()
	mainWin := a.NewWindow("Guic")
	mainWin.Resize(fyne.NewSize(800, 600))

	return &Application{
		app:     &a,
		mainWin: &mainWin,

		connectionWindow: connectionWindow,
		chatWindow:       chatWindow,
	}
}

type ChatRegistry struct {
	byUUID map[uuid.UUID]*regEntry
	byId   map[widget.ListItemID]*regEntry
	mu     sync.Mutex
}

type regEntry struct {
	Chat *core.Chat
	Lii  widget.ListItemID

	TextScroll *container.Scroll
}

func NewChatRegistry() *ChatRegistry {
	return &ChatRegistry{
		byUUID: make(map[uuid.UUID]*regEntry, 32),
		byId:   make(map[widget.ListItemID]*regEntry, 32),
	}
}

func (cr *ChatRegistry) Len() int {
	return len(cr.byId)
}

func (cr *ChatRegistry) AddChat(c *core.Chat) *regEntry {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	lii := cr.Len()
	entry := &regEntry{
		Chat:       c,
		Lii:        lii,
		TextScroll: container.NewVScroll(container.NewVBox()),
	}
	cr.byUUID[c.Id] = entry
	cr.byId[lii] = entry
	return entry
}

func (cr *ChatRegistry) RemoveChatById(i widget.ListItemID) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	c, ok := cr.byId[i]
	if ok {
		delete(cr.byId, i)
		delete(cr.byUUID, c.Chat.Id)
	}
}

func (cr *ChatRegistry) RemoveChatByUUID(id uuid.UUID) (widget.ListItemID, bool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	c, ok := cr.byUUID[id]
	if !ok {
		return -1, ok
	}
	delete(cr.byUUID, id)
	delete(cr.byId, c.Lii)
	return c.Lii, ok
}

func (cr *ChatRegistry) GetByItemId(i widget.ListItemID) (*regEntry, bool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	c, ok := cr.byId[i]
	return c, ok
}

func (cr *ChatRegistry) GetByUUID(id uuid.UUID) (*regEntry, bool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	e, ok := cr.byUUID[id]
	return e, ok
}
