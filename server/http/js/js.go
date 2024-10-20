package js

import (
	"fmt"
	"net/http"

	connection "github.com/Quartinal/wisp-server-go/connection/js"
	"github.com/Quartinal/wisp-server-go/logging"
	"github.com/Quartinal/wisp-server-go/options"
)

func HandleWebSocketJS(w http.ResponseWriter, r *http.Request, ws *connection.WebSocketReadWriter) {
	logging.Info(fmt.Sprintf("New connection from %s", r.RemoteAddr))

	opt := &options.OptionsStruct{}

	conn := connection.NewServerConnectionJS(ws, r.URL.Path, opt)
	if err := conn.Setup(); err != nil {
		logging.Error(fmt.Sprintf("Error setting up connection: %v", err))
		return
	}

	if err := conn.Run(); err != nil {
		logging.Error(fmt.Sprintf("Error running connection: %v", err))
	}

	logging.Info(fmt.Sprintf("Connection closed from %s", r.RemoteAddr))
}