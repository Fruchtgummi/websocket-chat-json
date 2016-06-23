package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	schreibZeit        = 10 * time.Second
	pongZeit           = 60 * time.Second
	pingLoop           = (pongZeit * 9) / 10
	maxNachrichtenSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type mess struct {
	data map[string]interface{}
	room int
}

type subs struct {
	li   *Link
	room int
}

type Orbit struct {
	verteiler chan mess
	anmelden  chan subs
	abmelden  chan subs

	rooms map[int]map[*Link]bool
}

var orbit = Orbit{

	verteiler: make(chan mess),
	anmelden:  make(chan subs),
	abmelden:  make(chan subs),

	rooms: make(map[int]map[*Link]bool),
}

type Link struct {
	webs     *websocket.Conn
	transmit chan map[string]interface{}
	room     int
}

func (o *Orbit) start() {

	for {
		select {
		case link := <-o.anmelden:
			verbindungen := o.rooms[link.room]

			if verbindungen == nil {
				verbindungen = make(map[*Link]bool)
				o.rooms[link.room] = verbindungen
			}

			o.rooms[link.room][link.li] = true

		case link := <-o.abmelden:

			verbindungen := o.rooms[link.room]
			if verbindungen != nil {
				if _, ok := verbindungen[link.li]; ok {
					delete(verbindungen, link.li)

					close(link.li.transmit)
					if len(verbindungen) == 0 {
						delete(o.rooms, link.room)
					}
				}
			}

		case m := <-o.verteiler:
			verbindungen := o.rooms[m.room]
			for link := range verbindungen {
				select {
				case link.transmit <- m.data:
				default:
					close(link.transmit)
					delete(verbindungen, link)

				}
			}
		}
	}

}

func (s *subs) schreiben(mt int, p []byte) error {
	log.Println(mt, p)
	s.li.webs.SetWriteDeadline(time.Now().Add(schreibZeit))
	return s.li.webs.WriteMessage(mt, p)
}

func (s *subs) schreibeWsJSON(r *http.Request) {

	l := s.li

	ticker := time.NewTicker(pingLoop)
	defer func() {
		ticker.Stop()
		l.webs.Close()
	}()

	for {
		select {

		case message, ok := <-l.transmit:
			if !ok {
				s.schreiben(websocket.CloseMessage, []byte{})
			}

			l.webs.SetWriteDeadline(time.Now().Add(schreibZeit)) //sau wichtig
			l.webs.WriteJSON(message)

		case <-ticker.C:
			if err := s.schreiben(websocket.PingMessage, []byte{}); err != nil {
				return
			}

		}

	}

}

func (s *subs) leseWsJSON() {

	l := s.li

	json := map[string]interface{}{}

	ticker := time.NewTicker(pingLoop)
	defer func() {
		ticker.Stop()
		l.webs.Close()
	}()

	l.webs.SetReadLimit(maxNachrichtenSize)
	l.webs.SetReadDeadline(time.Now().Add(pongZeit))
	l.webs.SetPongHandler(func(string) error { l.webs.SetReadDeadline(time.Now().Add(pongZeit)); return nil })

	for {
		err := l.webs.ReadJSON(&json)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}

		m := mess{json, s.room}
		orbit.verteiler <- m
	}
}

func cWs(w http.ResponseWriter, r *http.Request) {
	cws, err := upgrader.Upgrade(w, r, nil)

	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Kein Websockhandshake", 400)
		return
	} else if err != nil {
		log.Println(err)
		return
	}

	log.Println("Erfolgreich, erneuere Verbindung")

	link := &Link{transmit: make(chan map[string]interface{}), webs: cws, room: GetSessionValueInt(r, "project")}
	s := subs{link, GetSessionValueInt(r, "project")}
	orbit.anmelden <- s
	go s.schreibeWsJSON(r)
	s.leseWsJSON()
}
