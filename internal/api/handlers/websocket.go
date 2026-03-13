package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// WSEvents streams all monitor events to connected clients
func (h *Handler) WSEvents(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := h.mon.Subscribe()
	defer h.mon.Unsubscribe(ch)

	// Ping ticker to keep connection alive
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"service_id": ev.ServiceID,
				"type":       ev.Type,
				"message":    ev.Message,
				"time":       ev.Time.Format(time.RFC3339),
			})
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// WSLogs streams docker/process logs for a given service ID
func (h *Handler) WSLogs(c *gin.Context) {
	id := c.Param("id")
	svc, err := h.db.GetService(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var cmd *exec.Cmd
	switch svc.Type {
	case "docker":
		name := svc.ContainerName
		if name == "" {
			name = svc.Name
		}
		cmd = exec.Command("docker", "logs", "--follow", "--tail", "100", name)
	case "compose":
		cf := svc.ComposeFile
		if cf == "" {
			cf = "docker-compose.yml"
		}
		cmd = exec.Command("docker", "compose", "-f", cf, "logs", "--follow", "--tail", "100")
	case "systemd":
		unit := svc.SystemdUnit
		if unit == "" {
			unit = svc.Name + ".service"
		}
		cmd = exec.Command("journalctl", "-u", unit, "-f", "-n", "100")
	default:
		writeWS(conn, fmt.Sprintf("Log streaming not supported for service type: %s\n", svc.Type))
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeWS(conn, "failed to open log pipe: "+err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		writeWS(conn, "failed to start log process: "+err.Error())
		return
	}
	defer cmd.Process.Kill()

	// Close when client disconnects
	done := make(chan struct{})
	go func() {
		conn.ReadMessage() // block until client closes
		close(done)
	}()

	buf := make([]byte, 4096)
	for {
		select {
		case <-done:
			return
		default:
			n, err := stdout.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
}

func writeWS(conn *websocket.Conn, msg string) {
	conn.WriteMessage(websocket.TextMessage, []byte(msg))
}
