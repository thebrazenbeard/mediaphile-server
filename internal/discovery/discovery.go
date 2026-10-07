package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

const Protocol = "mediaphile-discovery"

type Info struct {
	ServerID     string   `json:"serverId"`
	Name         string   `json:"name"`
	HTTPPort     int      `json:"httpPort"`
	APIVersions  []string `json:"apiVersions"`
	Capabilities []string `json:"capabilities"`
}

type request struct {
	Protocol string `json:"protocol"`
	Version  int    `json:"version"`
	Type     string `json:"type"`
	ClientID string `json:"clientId"`
}

type response struct {
	Protocol string `json:"protocol"`
	Version  int    `json:"version"`
	Type     string `json:"type"`
	Info
}

func HandlePacket(data []byte, info Info) ([]byte, bool) {
	var req request
	if json.Unmarshal(data, &req) != nil || req.Protocol != Protocol || req.Version != 1 || req.Type != "discover" || req.ClientID == "" {
		return nil, false
	}
	out, err := json.Marshal(response{Protocol: Protocol, Version: 1, Type: "server", Info: info})
	return out, err == nil
}

func Serve(ctx context.Context, address string, info Info) error {
	conn, err := net.ListenPacket("udp4", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	buf := make([]byte, 2048)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			return err
		}
		if out, ok := HandlePacket(buf[:n], info); ok {
			_, _ = conn.WriteTo(out, peer)
		}
	}
}
