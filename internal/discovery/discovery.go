// Package discovery lets a 3DS find Big Top without typing an address: the console broadcasts
// "CLOWNSEC-BIGTOP?" on the network's UDP port 32501 and every Big Top answers with a line of
// JSON saying who it is and which port its HTTP side is on.
package discovery

import (
	"encoding/json"
	"log"
	"net"
	"strings"
)

const Question = "CLOWNSEC-BIGTOP?"

type Answer struct {
	Service string `json:"service"` // "clownsec-bigtop"
	Name    string `json:"name"`
	ID      string `json:"id"`
	Port    int    `json:"port"`
	Version string `json:"version"`
}

// Serve answers discovery questions until the socket fails. who is called per question so a
// renamed server answers with its new name.
func Serve(port int, who func() Answer) error {
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return err
	}
	defer pc.Close()
	buf := make([]byte, 512)
	for {
		n, from, err := pc.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(buf[:n]), Question) {
			continue
		}
		b, _ := json.Marshal(who())
		if _, err := pc.WriteToUDP(b, from); err != nil {
			log.Printf("discovery: answer to %v: %v", from, err)
		}
	}
}
