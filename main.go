package main

import (
	"log"

	"github.com/derekchuank/dht-metadata/dht"
)

func main() {
	log.Println("Starting.")

	dht := dht.InitializeDht()
	log.Printf("Dht status: %s", dht.YieldStatus())

	dht.FarmStep()

	dht.FreeDht()
}
