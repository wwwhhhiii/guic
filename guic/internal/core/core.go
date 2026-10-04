package core

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"log/slog"

	"github.com/pion/webrtc/v4"
)

type controlMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// should be sent from peer when connected
type controlInitPayload struct {
	PeerName string `json:"PeerName"`
	ChatName string `json:"ChatName"`
}

func NewControlInitMessage(peerName string, chatName string) (*controlMessage, error) {
	initp := &controlInitPayload{PeerName: peerName, ChatName: chatName}
	payload, err := json.Marshal(initp)
	if err != nil {
		return nil, err
	}
	cmsg := &controlMessage{
		Type:    "0",
		Payload: payload,
	}
	return cmsg, nil
}

func SetupAcceptor(
	recvMessage chan *Message,
	peerDisconnected chan *Peer,
	cfg webrtc.Configuration,
	offer string,
	myName string,
) (*Peer, *webrtc.SessionDescription, error) {
	sdp, err := DecodeSDP(offer)
	if err != nil {
		return nil, nil, err
	}
	c, err := webrtc.NewPeerConnection(cfg)
	if err != nil {
		return nil, nil, err
	}
	peer, err := setupAcceptorConnection(c, recvMessage, peerDisconnected, myName)
	if err != nil {
		return nil, nil, err
	}
	if err := c.SetRemoteDescription(*sdp); err != nil {
		return nil, nil, err
	}
	answer, err := c.CreateAnswer(nil)
	if err != nil {
		return nil, nil, err
	}
	if err = c.SetLocalDescription(answer); err != nil {
		return nil, nil, err
	}
	<-webrtc.GatheringCompletePromise(c)
	remote, err := c.RemoteDescription().Unmarshal()
	if err != nil {
		return nil, nil, err
	}
	for _, d := range remote.MediaDescriptions {
		for _, a := range d.Attributes {
			if a.IsICECandidate() {
				err := c.AddICECandidate(webrtc.ICECandidateInit{Candidate: a.String()})
				if err != nil {
					slog.Error("add ICE candidate", "error", err)
				} else {
					slog.Debug("add ICE candidate", "candidate", a.String())
				}
			}
		}
	}
	return peer, &answer, nil
}

func setupAcceptorConnection(
	c *webrtc.PeerConnection,
	msgOut chan<- *Message,
	peerDisconnected chan<- *Peer,
	myName string,
) (*Peer, error) {
	// when setting up offeree he always connects to a new chat
	p := NewPeer(c, CreateChat("", false))

	c.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Debug("connection state", "peer", p, "state", state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			p.addFlag(connectedFlag)
		case webrtc.PeerConnectionStateDisconnected:
			peerDisconnected <- p
		case webrtc.PeerConnectionStateClosed:
			peerDisconnected <- p
		case webrtc.PeerConnectionStateFailed:
			peerDisconnected <- p
		}
	})

	c.OnDataChannel(func(dc *webrtc.DataChannel) {
		slog.Debug("new data channel", "label", dc.Label())

		switch dc.Label() {
		case "ctrl":
			p.CtrlChan = dc
			setupAcceptorCtrlChan(p, dc, myName)
			p.addFlag(hasCtrlFlag)
		case "msg":
			p.MsgChan = dc
			setupAcceptorMsgChan(p, dc, msgOut)
			p.addFlag(hasMsgFlag)
		case "img":
			p.ImgChan = dc
			setupAcceptorImgChan(dc)
			p.addFlag(hasImgFlag)
		default:
			log.Fatalln("unknown channel label")
		}
	})

	return p, nil
}

func setupAcceptorCtrlChan(p *Peer, ch *webrtc.DataChannel, myName string) {
	ch.OnOpen(func() {
		// chat name is irrelevant here
		// because offeree joins chat
		cm, err := NewControlInitMessage(myName, "")
		if err != nil {
			slog.Error("could not create offeree control init message", "error", err)
			return
		}
		cmData, err := json.Marshal(cm)
		if err != nil {
			slog.Error("could not marshal offeree control init message", "error", err)
			return
		}
		if err := p.CtrlChan.Send(cmData); err != nil {
			slog.Error("could not send offeree control message", "error", err)
			return
		}
	})

	ch.OnMessage(func(m webrtc.DataChannelMessage) {
		slog.Debug("channel msg recieved", "name", p.CtrlChan.Label(), "text", string(m.Data))
		cm := &controlMessage{}
		if err := json.Unmarshal(m.Data, cm); err != nil {
			slog.Error("could not unmarshal control message", "error", err)
			return
		}
		switch cm.Type {
		case "0":
			init := &controlInitPayload{}
			if err := json.Unmarshal(cm.Payload, init); err != nil {
				slog.Error("could not unmarshal init control message payload", "error", err, "payload", cm.Payload)
				return
			}
			p.Name = init.PeerName
			p.addFlag(hasNameFlag)
			p.Chat.Name = init.ChatName
		default:
			slog.Error("unknown control message type", "type", cm.Type, "payload", cm.Payload)
		}
	})
}

func setupAcceptorMsgChan(p *Peer, ch *webrtc.DataChannel, msgOut chan<- *Message) {
	ch.OnMessage(func(m webrtc.DataChannelMessage) {
		slog.Debug("channel msg received", "name", ch.Label(), "data", m.Data)
		msg := &Message{}
		if err := json.Unmarshal(m.Data, msg); err != nil {
			slog.Error("message read", "error", err)
			return
		}
		// TODO chat id should be separate from message
		msg.ChatId = p.Chat.Id
		msgOut <- msg
	})
}

func setupAcceptorImgChan(ch *webrtc.DataChannel) {}

func SetupInitiator(
	recvMessage chan *Message,
	peerDisconnected chan *Peer,
	cfg webrtc.Configuration,
	chat *Chat,
	name string,
) (string, *Peer, error) {
	c, err := webrtc.NewPeerConnection(cfg)
	var offerstr string
	if err != nil {
		return offerstr, nil, err
	}
	peer, err := setupInitiator(c, chat, name, recvMessage)
	if err != nil {
		return offerstr, nil, err
	}
	offer, err := c.CreateOffer(nil)
	if err != nil {
		return offerstr, nil, err
	}
	if err = c.SetLocalDescription(offer); err != nil {
		return offerstr, nil, err
	}
	<-webrtc.GatheringCompletePromise(c)
	sdpdata, err := json.Marshal(*c.LocalDescription())
	if err != nil {
		return offerstr, nil, err
	}
	offerstr = base64.StdEncoding.EncodeToString(sdpdata)

	return offerstr, peer, nil
}

func setupInitiator(
	c *webrtc.PeerConnection,
	chat *Chat,
	name string,
	onMsgRecv chan<- *Message,
) (*Peer, error) {
	p := NewPeer(c, chat)

	var err error
	if p.CtrlChan, err = c.CreateDataChannel("ctrl", nil); err != nil {
		return nil, err
	}
	setupInitiatorCtrlChan(p, p.CtrlChan, name, chat.Name)
	if p.MsgChan, err = c.CreateDataChannel("msg", nil); err != nil {
		return nil, err
	}
	if p.ImgChan, err = c.CreateDataChannel("img", nil); err != nil {
		return nil, err
	}

	c.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Debug("connection state", "peer", "", "state", state.String())
		if state == webrtc.PeerConnectionStateFailed {
			// TODO
		}
		if state == webrtc.PeerConnectionStateClosed {
		}
		if state == webrtc.PeerConnectionStateConnected {
			p.addFlag(connectedFlag)
		}
		if state == webrtc.PeerConnectionStateDisconnected {
		}
	})

	p.MsgChan.OnOpen(func() {
		slog.Debug("channel opened", "name", p.MsgChan.Label())
		p.addFlag(hasMsgFlag)
	})
	p.MsgChan.OnClose(func() {
		slog.Debug("channel closed", "name", p.MsgChan.Label())
	})
	p.MsgChan.OnMessage(func(m webrtc.DataChannelMessage) {
		slog.Debug("channel msg recieved", "name", p.MsgChan.Label(), "text", string(m.Data), "peer", p.Name)
		msg := &Message{}
		if err := json.Unmarshal(m.Data, msg); err != nil {
			slog.Error("message read", "error", err)
			return
		}
		msg.ChatId = p.Chat.Id
		onMsgRecv <- msg
		if p.Chat.IsHosted {
			slog.Debug("broadcasting message", "from", p.Name, "chat", p.Chat.Name)
			p.Chat.SendMessage(msg)
		}
	})

	p.ImgChan.OnOpen(func() {
		slog.Debug("channel opened", "name", p.ImgChan.Label())
		p.addFlag(hasImgFlag)
	})
	p.ImgChan.OnClose(func() {
		slog.Debug("channel closed", "name", p.ImgChan.Label())
	})
	p.ImgChan.OnMessage(func(msg webrtc.DataChannelMessage) {
		slog.Debug("channel msg recieved", "name", p.ImgChan.Label(), "data", msg.Data, "peer", p.Name)
	})

	return p, nil

}

func setupInitiatorCtrlChan(p *Peer, ch *webrtc.DataChannel, name string, chatName string) {
	ch.OnOpen(func() {
		slog.Debug("channel opened", "name", p.CtrlChan.Label())
		p.addFlag(hasCtrlFlag)
		initmsg, err := NewControlInitMessage(name, chatName)
		if err != nil {
			slog.Error("could not create init message", "error", err)
			return
		}
		initData, err := json.Marshal(initmsg)
		if err != nil {
			slog.Error("could not marshal init message", "error", err)
			return
		}
		if err := p.CtrlChan.Send(initData); err != nil {
			slog.Error("error sending init data")
		}
	})

	p.CtrlChan.OnClose(func() {
		slog.Debug("channel closed", "name", p.CtrlChan.Label())
	})

	p.CtrlChan.OnMessage(func(m webrtc.DataChannelMessage) {
		slog.Debug("channel msg recieved", "name", p.CtrlChan.Label(), "text", string(m.Data), "name", p.Name)
		cm := &controlMessage{}
		if err := json.Unmarshal(m.Data, cm); err != nil {
			slog.Error("could not unmarshal control message", "error", err)
			return
		}
		switch cm.Type {
		case "0":
			init := &controlInitPayload{}
			if err := json.Unmarshal(cm.Payload, init); err != nil {
				slog.Error("could not unmarshal init control message payload", "error", err, "payload", cm.Payload)
				return
			}
			p.Name = init.PeerName
			p.addFlag(hasNameFlag)
			// not setting chat name because offerror provides chat name
		default:
			slog.Error("unknown control message type", "type", cm.Type, "payload", cm.Payload)
			return
		}
	})
}
