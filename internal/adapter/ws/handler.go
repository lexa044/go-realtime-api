package ws

import (
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/lexa044/realtime-api/internal/contextutil"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Restrict this to known origins in production — reflecting all
	// origins is only acceptable for local dev.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler upgrades an authenticated HTTP request to a websocket connection
// and registers the resulting client with the hub. Run auth (JWT, session
// cookie...) as middleware BEFORE this handler.
func Handler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(contextutil.UserIDKey).(string)

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		client := NewClient(hub, conn, userID)
		hub.register <- client

		go client.writePump()
		go client.readPump()
	}
}
