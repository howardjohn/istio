package main

import (
	"istio.io/istio/unshare"
	"log"
	"net"
	"os"

	_ "istio.io/istio/unshare"
)

func main() {
	log.Println(os.Getuid())
	err := unshare.Network()
	log.Println(err)
	l, err := net.Listen("tcp", "127.0.0.1:80")
	log.Println(err)
	go net.Dial("tcp", "127.0.0.1:80")
	_, err = l.Accept()
	log.Println("accepted", err)
}
